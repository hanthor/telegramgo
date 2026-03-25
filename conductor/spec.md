# Specification: Telegram Supergroup & Topic Relay Bridge

## 1. Goal and Overview

This specification outlines the architecture and implementation strategy for adding comprehensive relay functionality to the `telegramgo` bridge (a v3 rewrite of `mautrix-telegram` using `bridgev2`). 

The primary objective is to allow seamless bridging of **Telegram Supergroups with Topics** into a **Matrix Space** containing **Matrix Rooms**, enabling Matrix users without Telegram accounts to fully participate in the community via a Relay Bot. 

This implementation will draw inspiration from the `mautrix-slack` and `mautrix-discord` bridges, while adapting to Telegram's unique API constraints, aiming for a clean fork that rebases effortlessly from upstream.

---

## 2. Architectural Mapping

To provide a native experience on both ends, the structural mapping must mirror Telegram's forum mechanics:

- **Telegram Supergroup** ↔ **Matrix Space**
- **Telegram Topic (Thread)** ↔ **Matrix Room** (Sub-room linked to the Space)
- **Telegram General Topic** ↔ **Primary Matrix Room** (Often acts as the root of the Space)

### Space & Topic Lifecycle
1. When the bridge joins a Supergroup with topics enabled, it will provision a Matrix Space.
2. Existing Topics will be backfilled as Matrix Rooms within the Space.
3. New Topics created in Telegram will dynamically spawn new Matrix Rooms within the Space.

---

## 3. Login Flows & Relay Configuration

Unlike Discord or Slack where Webhooks are seamlessly created in the background, Telegram requires a standard Bot or User account to relay messages. 

### 3.1. Bot Account Flow (Recommended)
1. **Login:** A Matrix bridge administrator authenticates a Telegram Bot using the standard Bot Token (`login bot <token>`).
2. **Setup:** The admin invites the Bot to the target Telegram Supergroup and grants it necessary permissions (Manage Topics, Send Messages, Send Media, etc.).
3. **Activation:** The admin configures the Supergroup portal to enable Relay mode, assigning the bot as the default relay user.

### 3.2. User Account Flow (Fallback)
1. **Login:** An administrator logs in with a standard Telegram User account via Phone/QR Code.
2. **Setup:** The user enables Relay mode for the portal. The bridge uses this user's session to proxy messages for unauthenticated Matrix users.
3. *Note: Using a Bot is heavily preferred to avoid user account rate limits and confusion regarding the message sender.*

---

## 4. Message Formatting & Types (Matrix → Telegram)

Because the Telegram Bot API lacks a webhook equivalent to spoof the username and avatar per-message (like Slack/Discord), the relay must format the message body to represent the Matrix sender clearly.

### 4.1. Text Messages
- **Format:** `<b>[Display Name]</b>: <message_body>`
- Matrix usernames and avatars cannot be spoofed at the API level. The bridge will prepend the sender's Matrix Display Name using bold HTML/Markdown.

### 4.2. Media (Images, Video, Audio, Files)
- Media is downloaded from Matrix and re-uploaded directly to Telegram.
- **Caption:** The sender's identity is injected into the caption. `<b>[Display Name]</b>: <original_caption_if_any>`

### 4.3. Replies
- Matrix replies to ghost users (Telegram users) will be bridged as **native Telegram replies** to the original Telegram message.
- Matrix replies to other relayed Matrix users will reply to the bot's relayed message. 

### 4.4. Reactions
- **Constraint:** Telegram bots can only apply a single reaction to a message. Standard user accounts cannot react on behalf of others.
- **Implementation:** 
  - By default, reactions from relayed Matrix users will be dropped to avoid bot reaction clashing.
  - An optional configuration toggle (`relay_reactions_as_text: true`) can be added to emit reactions as fallback text: `<i>[Display Name] reacted with 👍</i>`.

### 4.5. Edits and Deletions
- **Edits:** If a Matrix user edits their message, the bridge will edit the Telegram message sent by the bot. It will reconstruct the message: `<b>[Display Name]</b>: <new_message_body>`.
- **Deletions:** If a Matrix user redacts their message, the bridge will delete the bot's message on Telegram.

---

## 5. Message Handling (Telegram → Matrix)

This direction remains functionally identical to standard Mautrix puppeting behavior.
- **Ghost Users:** Telegram users appearing in the Matrix Space will be represented by Ghost Users.
- Their messages will seamlessly appear in the correct topic rooms with their native Telegram display names and avatars.

---

## 6. Implementation Strategy for a Clean Fork

To ensure this fork remains clean and rebases easily over the upstream `mautrix-telegram` (bridgev2) repository, changes should be modular and loosely coupled:

### 6.1. Configuration (`pkg/connector/config.go`)
Add a `Relay` struct inside `TelegramConfig`:
```go
type RelayConfig struct {
    Enabled          bool   `yaml:"enabled"`
    MessageFormat    string `yaml:"message_format"` // default: "<b>{{ .SenderName }}</b>: {{ .Message }}"
    RelayReactions   bool   `yaml:"relay_reactions"`
}
```

### 6.2. Bridgev2 Relay mechanics
Hook into `bridgev2`'s native sender validation in `pkg/connector/handlematrix.go`:
- Inside `HandleMatrixMessage`, verify if `msg.OrigSender` has a valid Telegram login. 
- If no login is found and `Relay.Enabled` is true, fallback to the portal's designated bot/relay login.
- Pass a `isRelay bool` flag to the message converter to wrap the message text with the `MessageFormat` template.

### 6.3. Message Converter (`pkg/connector/tomatrix.go` & `telegramfmt`)
- Inject the relay formatting during the conversion process before the `gotd` API call is made. 
- Modify the caption generation for media uploads to include the same prefix.

### 6.4. Topic & Space Sync (`pkg/connector/chatsync.go`)
- Ensure the synchronization logic correctly assigns the `bridgev2.Portal` room type to `m.space` for Supergroups, and marks Topics as standard rooms with the `m.space.child` state event pointing back to the Supergroup.

## 7. Migration & Upgrades
No major database schema changes should be required as `bridgev2` inherently handles multiple logins and portal hierarchies. Relay toggles will be stored dynamically or in the bridge configuration, minimizing upstream database conflicts.