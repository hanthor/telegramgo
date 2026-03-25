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

import (
	"fmt"
	"slices"
	"strconv"
	"sync"
	"time"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/commands"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"

	"go.mau.fi/mautrix-telegram/pkg/connector/ids"
	"go.mau.fi/mautrix-telegram/pkg/gotd/tg"
)

var cmdSync = &commands.FullHandler{
	Func: fnSync,
	Name: "sync",
	Help: commands.HelpMeta{
		Section:     commands.HelpSectionChats,
		Description: "Synchronize your chat portals, contacts and/or own info.",
		Args:        "[`chats`|`contacts`|`me`|`topics`]",
	},
	RequiresLogin: true,
}

func fnSync(ce *commands.Event) {
	var only string
	if len(ce.Args) > 0 {
		if !slices.Contains([]string{"chats", "contacts", "me", "topics"}, ce.Args[0]) {
			ce.Reply("Invalid argument. Use `chats`, `contacts`, `me` or `topics`.")
			return
		}
		only = ce.Args[0]
	}

	if only == "topics" {
		if ce.Portal == nil {
			ce.Reply("You must be in a portal to synchronize its topics.")
			return
		}
		peerType, id, _, err := ids.ParsePortalID(ce.Portal.ID)
		if err != nil || peerType != ids.PeerTypeChannel {
			ce.Reply("This portal is not a channel/supergroup.")
			return
		}
		logins := ce.User.GetUserLogins()
		if len(logins) == 0 {
			ce.Reply("You are not logged in.")
			return
		}
		client := logins[0].Client.(*TelegramClient)
		ce.Reply("Synchronizing topics for this forum...")
		go func() {
			err := client.syncTopics(ce.Ctx, ce.Portal, id)
			if err != nil {
				ce.Reply("Failed to synchronize topics: %v", err)
			} else {
				ce.Reply("Topics synchronized successfully.")
			}
		}()
		return
	}

	var wg sync.WaitGroup
	for _, login := range ce.User.GetUserLogins() {
		client := login.Client.(*TelegramClient)
		if only == "" || only == "chats" {
			ce.Reply("Synchronizing chats for %s...", login.ID)
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := client.syncChats(ce.Ctx, 0, false, true); err != nil {
					ce.Reply("Failed to synchronize chats for %s: %v", login.ID, err)
				}
			}()
		}
		if only == "" || only == "contacts" {
			ce.Reply("Synchronizing contacts...")
			wg.Add(1)
			go func() {
				// TODO
				ce.Reply("Contact sync is not yet implemented!")
				defer wg.Done()
			}()
		}
		if only == "" || only == "me" {
			ce.Reply("Synchronizing your info...")
			wg.Add(1)
			go func() {
				defer wg.Done()
				if users, err := client.client.API().UsersGetUsers(ce.Ctx, []tg.InputUserClass{&tg.InputUserSelf{}}); err != nil {
					ce.Reply("Failed to get your info for %s: %v", login.ID, err)
				} else if len(users) == 0 {
					ce.Reply("Failed to get your info for %s: no users returned", login.ID)
				} else if users[0].TypeID() != tg.UserTypeID {
					ce.Reply("Unexpected user type %s", users[0].TypeName())
				} else if _, err = client.updateGhost(ce.Ctx, client.telegramUserID, users[0].(*tg.User)); err != nil {
					ce.Reply("Failed to update your info for %s: %v", login.ID, err)
				}
			}()
		}
	}
	wg.Wait()
}

var cmdPlumbTopic = &commands.FullHandler{
	Func: fnPlumbTopic,
	Name: "plumb-topic",
	Help: commands.HelpMeta{
		Section:     commands.HelpSectionChats,
		Description: "Link the current Matrix room to a Telegram topic.",
		Args:        "<channel_id> <topic_id>",
	},
	RequiresLogin: true,
}

func fnPlumbTopic(ce *commands.Event) {
	if len(ce.Args) < 2 {
		ce.Reply("Usage: `plumb-topic <channel_id> <topic_id>`")
		return
	}

	channelID, err := strconv.ParseInt(ce.Args[0], 10, 64)
	if err != nil {
		ce.Reply("Invalid channel ID.")
		return
	}
	topicID, err := strconv.Atoi(ce.Args[1])
	if err != nil {
		ce.Reply("Invalid topic ID.")
		return
	}

	portalKey := networkid.PortalKey{ID: ids.MakeTopicPortalID(channelID, topicID)}
	portal, err := ce.Bridge.GetPortalByKey(ce.Ctx, portalKey)
	if err != nil {
		ce.Reply("Failed to get portal: %v", err)
		return
	}

	if portal.MXID != "" {
		if portal.MXID == ce.RoomID {
			ce.Reply("This room is already linked to that topic.")
		} else {
			ce.Reply("That topic is already linked to another Matrix room: %s", portal.MXID)
		}
		return
	}

	existingPortal, err := ce.Bridge.GetPortalByMXID(ce.Ctx, ce.RoomID)
	if err != nil {
		ce.Reply("Failed to check if room is already linked: %v", err)
		return
	}
	if existingPortal != nil {
		ce.Reply("This room is already linked to another portal: %s. Unbridge it first if you want to re-link it.", existingPortal.ID)
		return
	}

	portal.MXID = ce.RoomID
	err = portal.Save(ce.Ctx)
	if err != nil {
		ce.Reply("Failed to save portal: %v", err)
		return
	}

	logins := ce.User.GetUserLogins()
	if len(logins) == 0 {
		ce.Reply("Successfully linked room, but you are not logged in to fetch topic info. You may need to manually sync the room later.")
		return
	}
	client := logins[0].Client.(*TelegramClient)
	info, err := client.GetChatInfo(ce.Ctx, portal)
	if err != nil {
		ce.Reply("Successfully linked room, but failed to fetch topic info: %v. You may need to manually sync the room.", err)
		return
	}

	portal.UpdateInfo(ce.Ctx, info, logins[0], nil, time.Time{})
	ce.Reply("Successfully linked this room to topic %d in channel %d.", topicID, channelID)
}

var cmdSetRelaySpace = &commands.FullHandler{
	Func: fnSetRelaySpace,
	Name: "set-relay-space",
	Help: commands.HelpMeta{
		Section:     commands.HelpSectionChats,
		Description: "Set the default relay on this portal and all its child topic rooms.",
	},
	RequiresPortal: true,
	RequiresAdmin:  true,
}

func fnSetRelaySpace(ce *commands.Event) {
	if !ce.Bridge.Config.Relay.Enabled {
		ce.Reply("This bridge does not allow relay mode")
		return
	}
	if len(ce.Bridge.Config.Relay.DefaultRelays) == 0 {
		ce.Reply("No default_relays configured in bridge config")
		return
	}

	// If run from a topic room, walk up to the parent space first
	spacePortal := ce.Portal
	if ce.Portal.ParentKey.ID != "" {
		parent, err := ce.Bridge.GetPortalByKey(ce.Ctx, ce.Portal.ParentKey)
		if err == nil && parent != nil {
			spacePortal = parent
		}
	}

	// Collect the space plus all children
	portals := []*bridgev2.Portal{spacePortal}
	children, err := ce.Bridge.DB.Portal.GetChildren(ce.Ctx, spacePortal.PortalKey)
	if err != nil {
		ce.Reply("Failed to get child portals: %v", err)
		return
	}
	for _, child := range children {
		p, err := ce.Bridge.GetPortalByKey(ce.Ctx, child.PortalKey)
		if err != nil || p == nil {
			continue
		}
		portals = append(portals, p)
	}

	// Find the relay login from default_relays
	logins, err := ce.Bridge.GetUserLoginsInPortal(ce.Ctx, ce.Portal.PortalKey)
	if err != nil {
		ce.Reply("Failed to get logins in portal: %v", err)
		return
	}
	var relay *bridgev2.UserLogin
	for _, loginID := range ce.Bridge.Config.Relay.DefaultRelays {
		for _, login := range logins {
			if login.ID == loginID {
				relay = login
				break
			}
		}
		if relay != nil {
			break
		}
	}
	if relay == nil {
		ce.Reply("None of the configured default relay users are in this portal")
		return
	}

	publicPortals := ce.Bridge.Network.(*TelegramConnector).Config.Relay.PublicPortals

	var set, failed int
	for _, p := range portals {
		if err := p.SetRelay(ce.Ctx, relay); err != nil {
			failed++
			continue
		}
		set++
		if publicPortals && p.MXID != "" {
			_, _ = ce.Bridge.Bot.SendState(ce.Ctx, p.MXID, event.StateJoinRules, "", &event.Content{
				Parsed: &event.JoinRulesEventContent{JoinRule: event.JoinRulePublic},
			}, time.Time{})
		}
	}
	ce.Reply(fmt.Sprintf("Set relay to %s on %d room(s)%s", relay.RemoteName, set,
		func() string {
			if failed > 0 {
				return fmt.Sprintf(", failed on %d", failed)
			}
			return ""
		}()))
}
