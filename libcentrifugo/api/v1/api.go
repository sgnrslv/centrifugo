package apiv1

import (
	"encoding/json"

	"github.com/centrifugal/centrifugo/libcentrifugo/logger"
	"github.com/centrifugal/centrifugo/libcentrifugo/node"
	"github.com/centrifugal/centrifugo/libcentrifugo/proto"
)

var (
	arrayJSONPrefix  byte = '['
	objectJSONPrefix byte = '{'
)

func APICommandsFromJSON(msg []byte) ([]proto.APICommand, error) {
	var cmds []proto.APICommand

	if len(msg) == 0 {
		return cmds, nil
	}

	firstByte := msg[0]

	switch firstByte {
	case objectJSONPrefix:
		// single command request
		var command proto.APICommand
		err := json.Unmarshal(msg, &command)
		if err != nil {
			return nil, err
		}
		cmds = append(cmds, command)
	case arrayJSONPrefix:
		// array of commands received
		err := json.Unmarshal(msg, &cmds)
		if err != nil {
			return nil, err
		}
	default:
		return nil, proto.ErrInvalidMessage
	}
	return cmds, nil
}

// APICmd builds API command and dispatches it into correct handler method.
func APICmd(n *node.Node, cmd proto.APICommand) (proto.Response, error) {

	var err error
	var resp proto.Response

	method := cmd.Method
	params := cmd.Params

	switch method {
	case "publish":
		var cmd proto.PublishAPICommand
		err = json.Unmarshal(params, &cmd)
		if err != nil {
			logger.ERROR.Println(err)
			return nil, proto.ErrInvalidMessage
		}
		resp, err = PublishCmd(n, &cmd)
	case "broadcast":
		var cmd proto.BroadcastAPICommand
		err = json.Unmarshal(params, &cmd)
		if err != nil {
			logger.ERROR.Println(err)
			return nil, proto.ErrInvalidMessage
		}
		resp, err = BroadcastCmd(n, &cmd)
	case "unsubscribe":
		var cmd proto.UnsubscribeAPICommand
		err = json.Unmarshal(params, &cmd)
		if err != nil {
			logger.ERROR.Println(err)
			return nil, proto.ErrInvalidMessage
		}
		resp, err = UnsubcribeCmd(n, &cmd)
	case "disconnect":
		var cmd proto.DisconnectAPICommand
		err = json.Unmarshal(params, &cmd)
		if err != nil {
			logger.ERROR.Println(err)
			return nil, proto.ErrInvalidMessage
		}
		resp, err = DisconnectCmd(n, &cmd)
	case "presence":
		var cmd proto.PresenceAPICommand
		err = json.Unmarshal(params, &cmd)
		if err != nil {
			logger.ERROR.Println(err)
			return nil, proto.ErrInvalidMessage
		}
		resp, err = PresenceCmd(n, &cmd)
	case "history":
		var cmd proto.HistoryAPICommand
		err = json.Unmarshal(params, &cmd)
		if err != nil {
			logger.ERROR.Println(err)
			return nil, proto.ErrInvalidMessage
		}
		resp, err = HistoryCmd(n, &cmd)
	case "channels":
		resp, err = ChannelsCmd(n)
	case "stats":
		resp, err = StatsCmd(n)
	case "node":
		resp, err = NodeCmd(n)
	default:
		return nil, proto.ErrMethodNotFound
	}
	if err != nil {
		return nil, err
	}

	resp.SetUID(cmd.UID)

	return resp, nil
}

// PublishCmd publishes data into channel.
func PublishCmd(n *node.Node, cmd *proto.PublishAPICommand) (proto.Response, error) {
	ch := cmd.Channel
	data := cmd.Data
	client := cmd.Client

	if string(ch) == "" || len(data) == 0 {
		return nil, proto.ErrInvalidMessage
	}

	resp := proto.NewAPIPublishResponse()

	chOpts, err := n.ChannelOpts(ch)
	if err != nil {
		resp.SetErr(proto.ResponseError{err, proto.ErrorAdviceFix})
		return resp, nil
	}

	message := proto.NewMessage(ch, data, client, nil)
	if chOpts.Watch {
		byteMessage, err := json.Marshal(message)
		if err != nil {
			logger.ERROR.Println(err)
		} else {
			n.PublishAdmin(proto.NewAdminMessage("message", byteMessage))
		}
	}

	err = <-n.Publish(message, &chOpts)

	if err != nil {
		resp.SetErr(proto.ResponseError{err, proto.ErrorAdviceNone})
		return resp, nil
	}
	return resp, nil
}

// BroadcastCmd publishes data into multiple channels.
func BroadcastCmd(n *node.Node, cmd *proto.BroadcastAPICommand) (proto.Response, error) {

	resp := proto.NewAPIBroadcastResponse()

	channels := cmd.Channels
	data := cmd.Data
	client := cmd.Client

	if len(channels) == 0 {
		logger.ERROR.Println("channels required for broadcast")
		resp.SetErr(proto.ResponseError{proto.ErrInvalidMessage, proto.ErrorAdviceFix})
		return resp, nil
	}

	if len(data) == 0 {
		logger.ERROR.Println("empty data")
		resp.SetErr(proto.ResponseError{proto.ErrInvalidMessage, proto.ErrorAdviceFix})
		return resp, nil
	}

	errs := make([]<-chan error, len(channels))
	for i, ch := range channels {

		if string(ch) == "" {
			resp.SetErr(proto.ResponseError{proto.ErrInvalidMessage, proto.ErrorAdviceFix})
			return resp, nil
		}

		chOpts, err := n.ChannelOpts(ch)
		if err != nil {
			resp.SetErr(proto.ResponseError{err, proto.ErrorAdviceFix})
			return resp, nil
		}

		message := proto.NewMessage(ch, data, client, nil)
		if chOpts.Watch {
			byteMessage, err := json.Marshal(message)
			if err != nil {
				logger.ERROR.Println(err)
			} else {
				n.PublishAdmin(proto.NewAdminMessage("message", byteMessage))
			}
		}

		errs[i] = n.Publish(message, &chOpts)
	}

	var firstErr error
	for i := range errs {
		err := <-errs[i]
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			logger.ERROR.Printf("Error publishing into channel %s: %v", string(channels[i]), err.Error())
		}
	}
	if firstErr != nil {
		resp.SetErr(proto.ResponseError{firstErr, proto.ErrorAdviceNone})
	}
	return resp, nil
}

// UnsubscribeCmd unsubscribes project's user from channel and sends
// unsubscribe control message to other nodes.
func UnsubcribeCmd(n *node.Node, cmd *proto.UnsubscribeAPICommand) (proto.Response, error) {

	resp := proto.NewAPIUnsubscribeResponse()

	user := cmd.User
	channel := cmd.Channel

	err := n.Unsubscribe(user, channel)
	if err != nil {
		resp.SetErr(proto.ResponseError{err, proto.ErrorAdviceNone})
		return resp, nil
	}
	return resp, nil
}

// DisconnectCmd disconnects user by its ID and sends disconnect
// control message to other nodes so they could also disconnect this user.
func DisconnectCmd(n *node.Node, cmd *proto.DisconnectAPICommand) (proto.Response, error) {

	resp := proto.NewAPIDisconnectResponse()

	user := cmd.User

	err := n.Disconnect(user, false)
	if err != nil {
		resp.SetErr(proto.ResponseError{err, proto.ErrorAdviceNone})
		return resp, nil
	}
	return resp, nil
}

// PresenceCmd returns response with presense information for channel.
func PresenceCmd(n *node.Node, cmd *proto.PresenceAPICommand) (proto.Response, error) {
	channel := cmd.Channel
	body := proto.PresenceBody{
		Channel: channel,
	}
	presence, err := n.Presence(channel)
	if err != nil {
		resp := proto.NewAPIPresenceResponse(body)
		resp.SetErr(proto.ResponseError{err, proto.ErrorAdviceNone})
		return resp, nil
	}
	body.Data = presence
	return proto.NewAPIPresenceResponse(body), nil
}

// HistoryCmd returns response with history information for channel.
func HistoryCmd(n *node.Node, cmd *proto.HistoryAPICommand) (proto.Response, error) {
	channel := cmd.Channel
	body := proto.HistoryBody{
		Channel: channel,
	}
	history, err := n.History(channel)
	if err != nil {
		resp := proto.NewAPIHistoryResponse(body)
		resp.SetErr(proto.ResponseError{err, proto.ErrorAdviceNone})
		return resp, nil
	}
	body.Data = history
	return proto.NewAPIHistoryResponse(body), nil
}

// ChannelsCmd returns active channels.
func ChannelsCmd(n *node.Node) (proto.Response, error) {
	body := proto.ChannelsBody{}
	channels, err := n.Channels()
	if err != nil {
		logger.ERROR.Println(err)
		resp := proto.NewAPIChannelsResponse(body)
		resp.SetErr(proto.ResponseError{proto.ErrInternalServerError, proto.ErrorAdviceNone})
		return resp, nil
	}
	body.Data = channels
	return proto.NewAPIChannelsResponse(body), nil
}

// StatsCmd returns active node stats.
func StatsCmd(n *node.Node) (proto.Response, error) {
	body := proto.StatsBody{}
	body.Data = n.Stats()
	return proto.NewAPIStatsResponse(body), nil
}

// NodeCmd returns simple counter metrics which update in real time for the current node only.
func NodeCmd(n *node.Node) (proto.Response, error) {
	body := proto.NodeBody{}
	body.Data = n.Node()
	return proto.NewAPINodeResponse(body), nil
}
