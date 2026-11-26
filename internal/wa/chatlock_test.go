package wa

import (
	"crypto/pbkdf2"
	"crypto/sha512"
	"testing"

	"github.com/polymorfa/hypermeow/appstate"
	"github.com/polymorfa/hypermeow/proto/waChatLockSettings"
	"github.com/polymorfa/hypermeow/proto/waSyncAction"
	"github.com/polymorfa/hypermeow/proto/waUserPassword"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"
)

// lockSecret is a secret code as phones sync it.
func lockSecret(t *testing.T, code string, enc waUserPassword.UserPassword_Encoding) *waUserPassword.UserPassword {
	salt := []byte("0123456789abcdef")
	key, err := pbkdf2.Key(sha512.New, string(lockCodeBytes(code, enc)), salt, 1000, 64)
	if err != nil {
		t.Fatal(err)
	}
	return &waUserPassword.UserPassword{
		Encoding:    enc.Enum(),
		Transformer: waUserPassword.UserPassword_PBKDF2_HMAC_SHA512.Enum(),
		TransformerArg: []*waUserPassword.UserPassword_TransformerArg{
			{Key: proto.String("iterations"), Value: &waUserPassword.UserPassword_TransformerArg_Value{
				Value: &waUserPassword.UserPassword_TransformerArg_Value_AsUnsignedInteger{AsUnsignedInteger: 1000}}},
			{Key: proto.String("salt"), Value: &waUserPassword.UserPassword_TransformerArg_Value{
				Value: &waUserPassword.UserPassword_TransformerArg_Value_AsBlob{AsBlob: salt}}},
		},
		TransformedData: key,
	}
}

// TestChatLockSync checks that a lock action locks the chat and unarchives
// it, and that the synced secret code is the one that opens Locked chats.
func TestChatLockSync(t *testing.T) {
	b := testBackend(t)
	ctx := b.ctx
	const chat = "555@lid"
	_ = b.store.setField(ctx, chat, "archived", 1)
	b.onChatLockAppState(&events.AppState{Index: []string{appstate.IndexLock, chat},
		SyncActionValue: &waSyncAction.SyncActionValue{LockChatAction: &waSyncAction.LockChatAction{Locked: proto.Bool(true)}}})
	var locked, archived int
	if err := b.store.db.QueryRowContext(ctx, `SELECT locked, archived FROM wz_chats WHERE jid = ?`, chat).Scan(&locked, &archived); err != nil {
		t.Fatal(err)
	}
	if locked != 1 || archived != 0 {
		t.Errorf("locked %d, archived %d", locked, archived)
	}

	if b.ChatLock().Code || b.CheckLockCode("1234") {
		t.Fatal("a code works before one is set")
	}
	b.onChatLockAppState(&events.AppState{Index: []string{appstate.IndexSettingChatLock},
		SyncActionValue: &waSyncAction.SyncActionValue{ChatLockSettings: &waChatLockSettings.ChatLockSettings{
			HideLockedChats: proto.Bool(true), SecretCode: lockSecret(t, "kunci ü", waUserPassword.UserPassword_UTF8)}}})
	if s := b.ChatLock(); !s.Code || !s.Hide {
		t.Errorf("settings %+v", s)
	}
	if !b.CheckLockCode("kunci ü") || b.CheckLockCode("kunci u") || b.CheckLockCode("") {
		t.Error("CheckLockCode")
	}
	// An old client's broken encoding.
	if pw := lockSecret(t, "ü", waUserPassword.UserPassword_UTF8_BROKEN); !checkLockCode(pw, "ü") || checkLockCode(pw, "u") {
		t.Error("UTF8_BROKEN")
	}
	if got := string(lockCodeBytes("aü", waUserPassword.UserPassword_UTF8_BROKEN)); got != "aￃﾼ" {
		t.Errorf("UTF8_BROKEN bytes %q", got)
	}
}
