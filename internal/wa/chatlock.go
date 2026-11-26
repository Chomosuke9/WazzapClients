package wa

import (
	"context"
	"crypto/pbkdf2"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base64"
	"hash"
	"unicode/utf8"

	"github.com/polymorfa/hypermeow/appstate"
	"github.com/polymorfa/hypermeow/proto/waChatLockSettings"
	"github.com/polymorfa/hypermeow/proto/waSyncAction"
	"github.com/polymorfa/hypermeow/proto/waUserPassword"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Chat lock, as WhatsApp Web has it (WAWebLockChatSync,
// WAWebChatLockSettingsSync, WAWebChatLockCrypto). A chat locked on the
// phone is a lockChatAction in app state (regular_low, index
// ["lock", jid]); history sync says it too (Conversation.locked). Its
// settings are one chatLockSettings action (["setting_chatLock"]): whether
// the Locked chats row is hidden, and the secret code that opens them on
// linked devices, as a PBKDF2 hash with its salt and iterations. A chat's
// lock is kept in wz_chats.locked, the settings in wz_meta.

// metaChatLock is the latest ChatLockSettings, base64, in wz_meta.
const metaChatLock = "chat_lock_settings"

// lockMutationVersion is the version WhatsApp Web sends lock actions with.
const lockMutationVersion = 7

// onChatLockAppState reads the app state actions of Chat lock.
func (b *Backend) onChatLockAppState(e *events.AppState) {
	if len(e.Index) == 0 {
		return
	}
	switch {
	case e.Index[0] == appstate.IndexLock && len(e.Index) > 1 && e.GetLockChatAction() != nil:
		jid, err := types.ParseJID(e.Index[1])
		if err != nil {
			return
		}
		locked := e.GetLockChatAction().GetLocked()
		if locked { // as WhatsApp Web does: a locked chat isn't archived
			b.updateChat(jid, "archived", 0, true)
		}
		b.updateChat(jid, "locked", boolInt(locked), false)
	case e.Index[0] == appstate.IndexSettingChatLock && e.GetChatLockSettings() != nil:
		raw, err := proto.Marshal(e.GetChatLockSettings())
		if err != nil {
			return
		}
		if err := b.store.setMetaValue(b.ctx, metaChatLock, base64.StdEncoding.EncodeToString(raw)); err != nil {
			b.log.Warnf("store chat lock settings: %v", err)
			return
		}
		b.emit(model.ChatLockEvent{})
	}
}

// chatLockSettings returns the stored settings, or nil.
func (b *Backend) chatLockSettings(ctx context.Context) *waChatLockSettings.ChatLockSettings {
	raw, err := base64.StdEncoding.DecodeString(b.store.meta(ctx, metaChatLock))
	if err != nil || len(raw) == 0 {
		return nil
	}
	var s waChatLockSettings.ChatLockSettings
	if proto.Unmarshal(raw, &s) != nil {
		return nil
	}
	return &s
}

func (b *Backend) ChatLock() model.ChatLock {
	s := b.chatLockSettings(b.ctx)
	return model.ChatLock{Code: len(s.GetSecretCode().GetTransformedData()) > 0, Hide: s.GetHideLockedChats()}
}

func (b *Backend) CheckLockCode(code string) bool {
	return checkLockCode(b.chatLockSettings(b.ctx).GetSecretCode(), code)
}

// checkLockCode derives code's key as the secret code's was and compares
// them (validateChatLockSecretCode).
func checkLockCode(pw *waUserPassword.UserPassword, code string) bool {
	want := pw.GetTransformedData()
	if len(want) == 0 || code == "" {
		return false
	}
	var h func() hash.Hash
	switch pw.GetTransformer() {
	case waUserPassword.UserPassword_PBKDF2_HMAC_SHA512:
		h = sha512.New
	case waUserPassword.UserPassword_PBKDF2_HMAC_SHA384:
		h = sha512.New384
	default:
		return false
	}
	var salt []byte
	var iter int
	for _, a := range pw.GetTransformerArg() {
		switch a.GetKey() {
		case "salt":
			salt = a.GetValue().GetAsBlob()
		case "iterations":
			iter = int(a.GetValue().GetAsUnsignedInteger())
		}
	}
	if salt == nil || iter <= 0 {
		return false
	}
	got, err := pbkdf2.Key(h, string(lockCodeBytes(code, pw.GetEncoding())), salt, iter, len(want))
	return err == nil && subtle.ConstantTimeCompare(got, want) == 1
}

// lockCodeBytes is the code as the secret code was hashed. UTF8_BROKEN is
// what an old client made of it: each UTF-8 byte read as a UTF-16 unit
// (0xFF00 added to those of 128 and over), encoded in UTF-8 again.
func lockCodeBytes(code string, enc waUserPassword.UserPassword_Encoding) []byte {
	if enc != waUserPassword.UserPassword_UTF8_BROKEN {
		return []byte(code)
	}
	var out []byte
	for i := 0; i < len(code); i++ {
		r := rune(code[i])
		if code[i] >= 128 {
			r |= 0xff00
		}
		out = utf8.AppendRune(out, r)
	}
	return out
}

func (b *Backend) SetLocked(chatID string, locked bool) {
	if locked {
		_ = b.store.setField(b.ctx, chatID, "archived", 0)
	}
	b.chatAction(chatID, "locked", boolInt(locked), func(j types.JID) appstate.PatchInfo {
		return appstate.PatchInfo{
			Type: appstate.WAPatchRegularLow,
			Mutations: []appstate.MutationInfo{{
				Index:   []string{appstate.IndexLock, j.String()},
				Version: lockMutationVersion,
				Value:   &waSyncAction.SyncActionValue{LockChatAction: &waSyncAction.LockChatAction{Locked: proto.Bool(locked)}},
			}},
		}
	})
}
