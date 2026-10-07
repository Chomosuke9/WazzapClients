package wa

import (
	"context"

	whatsmeow "github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
)

// sendMessage shares the app's send lock with the individual encryption used
// by whispers. Hypermeow's private SendMessage lock doesn't cover its exposed
// EncryptMessageForDevice primitive; concurrent sends could lose session state.
func (b *Backend) sendMessage(ctx context.Context, cli *whatsmeow.Client, to types.JID,
	msg *waE2E.Message, extra ...whatsmeow.SendRequestExtra,
) (whatsmeow.SendResponse, error) {
	b.sendMu.Lock()
	defer b.sendMu.Unlock()
	return cli.SendMessage(ctx, to, msg, extra...)
}
