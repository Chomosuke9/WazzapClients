package mock

import (
	"fmt"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// demoPackSize is how many stickers a demo pack has.
const demoPackSize = 6

// packs are the sticker packs opened, by ID, and added the ones added to
// the picker, in order.
type packs struct {
	opened map[string]*model.StickerPack
	added  []string
}

// OpenStickerPack hands out the pack m shares, its stickers blank.
func (b *Backend) OpenStickerPack(m *model.Message) {
	if m == nil || m.Pack == nil {
		return
	}
	b.mu.Lock()
	p := b.packs.opened[m.Pack.ID]
	if p == nil {
		cp := *m.Pack
		p = &cp
		if p.Name == "" {
			p.Name = p.ID
		}
		if p.Publisher == "" && p.FirstParty {
			p.Publisher = "WhatsApp"
		}
		p.Count = demoPackSize
		if b.packs.opened == nil {
			b.packs.opened = map[string]*model.StickerPack{}
		}
		b.packs.opened[p.ID] = p
	}
	cp := *p
	b.mu.Unlock()
	b.emit(model.StickerPackEvent{ID: m.Pack.ID, Pack: &cp, Stickers: b.PackStickers(p.ID)})
}

func (b *Backend) StickerPacks() []*model.StickerPack {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []*model.StickerPack
	for _, id := range b.packs.added {
		cp := *b.packs.opened[id]
		out = append(out, &cp)
	}
	return out
}

func (b *Backend) PackStickers(id string) []*model.Message {
	out := make([]*model.Message, demoPackSize)
	for i := range out {
		out[i] = &model.Message{ChatID: "stickers", ID: fmt.Sprintf("%s:%d", id, i), Kind: model.KindSticker, Media: model.MediaSticker}
	}
	return out
}

func (b *Backend) AddStickerPack(id string, add bool) {
	b.mu.Lock()
	p := b.packs.opened[id]
	if p != nil && p.Added != add {
		p.Added = add
		if add {
			b.packs.added = append(b.packs.added, id)
		} else {
			for i, a := range b.packs.added {
				if a == id {
					b.packs.added = append(b.packs.added[:i:i], b.packs.added[i+1:]...)
					break
				}
			}
		}
	}
	b.mu.Unlock()
	b.emit(model.StickersEvent{})
}

func (b *Backend) SetStickerPackOrder(ids []string) {
	b.mu.Lock()
	var order []string
	for _, id := range ids {
		if p := b.packs.opened[id]; p != nil && p.Added {
			order = append(order, id)
		}
	}
	if len(order) == len(b.packs.added) {
		b.packs.added = order
	}
	b.mu.Unlock()
	b.emit(model.StickersEvent{})
}
