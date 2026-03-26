// mautrix-telegram - A Matrix-Telegram puppeting bridge.
// Copyright (C) 2025 Sumner Evans
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package connector

// PoC: when a Matrix call starts in a relay portal, post a guest join link to Telegram.
//
// To remove this feature entirely: delete this file and remove the
// tg.registerCallHandlers() call from Init() in connector.go.
//
// Prerequisites:
//   - network.relay.call_links: true
//   - allow_guest_access: true in Synapse homeserver.yaml
//   - Rooms must have m.room.guest_access: can_join (set by set-relay-space when
//     network.relay.public_portals is also true)

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/url"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2/matrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-telegram/pkg/gotd/tg"
)

// eventMSC3401Call is the matrixRTC group call state event (used by Element Call).
var eventMSC3401Call = event.Type{Type: "org.matrix.msc3401.call", Class: event.StateEventType}

// eventCallInvite is the legacy 1:1 VoIP call invite event.
var eventCallInvite = event.Type{Type: "m.call.invite", Class: event.MessageEventType}

func (tc *TelegramConnector) registerCallHandlers() {
	if !tc.Config.Relay.CallLinks {
		return
	}
	mc, ok := tc.Bridge.Matrix.(*matrix.Connector)
	if !ok {
		return
	}
	serverName := mc.ServerName()
	handler := func(ctx context.Context, evt *event.Event) {
		tc.handleCallEvent(ctx, evt, serverName)
	}
	mc.EventProcessor.On(eventMSC3401Call, handler)
	mc.EventProcessor.On(eventCallInvite, handler)
}

func (tc *TelegramConnector) handleCallEvent(ctx context.Context, evt *event.Event, serverName string) {
	log := zerolog.Ctx(ctx).With().Str("component", "call_handler").Logger()

	// MSC3401: empty content or empty foci list means the call ended — skip.
	if evt.Type == eventMSC3401Call {
		if len(evt.Content.Raw) == 0 {
			return
		}
		if foci, ok := evt.Content.Raw["m.foci.active"]; ok {
			if arr, ok := foci.([]any); ok && len(arr) == 0 {
				return
			}
		}
	}

	portal, err := tc.Bridge.GetPortalByMXID(ctx, evt.RoomID)
	if err != nil || portal == nil || portal.Relay == nil {
		return
	}

	logins, err := tc.Bridge.GetUserLoginsInPortal(ctx, portal.PortalKey)
	if err != nil || len(logins) == 0 {
		log.Warn().Err(err).Stringer("room_id", evt.RoomID).Msg("No logins found, cannot post call link")
		return
	}

	var client *TelegramClient
	for _, login := range logins {
		if c, ok := login.Client.(*TelegramClient); ok {
			client = c
			break
		}
	}
	if client == nil {
		return
	}

	peer, _, err := client.inputPeerForPortalID(ctx, portal.ID)
	if err != nil {
		log.Err(err).Msg("Failed to get Telegram peer for portal")
		return
	}

	callURL := buildCallURL(evt.RoomID, serverName)
	msg := fmt.Sprintf("📞 A call has started. Join here:\n%s", callURL)

	_, err = client.client.API().MessagesSendMessage(ctx, &tg.MessagesSendMessageRequest{
		Peer:     peer,
		Message:  msg,
		RandomID: rand.Int64(),
	})
	if err != nil {
		log.Err(err).Msg("Failed to send call link to Telegram")
	}
}

func buildCallURL(roomID id.RoomID, serverName string) string {
	return fmt.Sprintf("https://call.element.io/room/#?roomId=%s&homeserverUrl=%s",
		url.QueryEscape(string(roomID)),
		url.QueryEscape("https://"+serverName))
}
