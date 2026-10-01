package mock

import (
	"time"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// demoAccount is the demo account's profile and settings.
func (b *Backend) demoAccount() *model.Account {
	if b.acc == nil {
		b.acc = &model.Account{
			ID: "15550100@s.whatsapp.net", Phone: "+1 555 0100", LID: "me@lid",
			Name: b.me(), About: "Hey there! I am using WhatsApp.",
			Linked: b.now().AddDate(0, -2, -3),
			Privacy: map[string]string{
				model.PrivacyLastSeen:     model.WhoContacts,
				model.PrivacyOnline:       model.WhoEveryone,
				model.PrivacyPhoto:        model.WhoContacts,
				model.PrivacyAbout:        model.WhoEveryone,
				model.PrivacyGroups:       model.WhoContacts,
				model.PrivacyReadReceipts: model.WhoEveryone,
			},
			TimerKnown:   true,
			BlockedKnown: true,
			Blocked:      []model.Contact{{ID: "spam1", Name: "+1 555 0199"}},
		}
	}
	return b.acc
}

func (b *Backend) me() string {
	if b.meName != "" {
		return b.meName
	}
	return "Me Myself"
}

// Account returns a copy, as the real backend does.
func (b *Backend) Account() *model.Account {
	a := *b.demoAccount()
	return &a
}

func (b *Backend) SetProfileName(name string) {
	b.demoAccount().Name = name
	b.meName = name
	b.emit(model.AccountEvent{})
}

func (b *Backend) SetAbout(about string) {
	b.demoAccount().About = about
	b.emit(model.AccountEvent{})
}

func (b *Backend) SetProfilePhoto(string) {
	b.emit(model.NoticeEvent{Text: "Demo mode doesn't change your profile photo."})
}

func (b *Backend) SetPrivacy(key, value string) {
	a := b.demoAccount()
	p := make(map[string]string, len(a.Privacy))
	for k, v := range a.Privacy {
		p[k] = v
	}
	p[key] = value
	a.Privacy = p
	b.emit(model.AccountEvent{})
}

func (b *Backend) SetDefaultTimer(d time.Duration) {
	b.demoAccount().DefaultTimer = d
	b.emit(model.AccountEvent{})
}

// setBlockedContact keeps the blocked contacts in step with SetBlocked.
func (b *Backend) setBlockedContact(id string, blocked bool) {
	a := b.demoAccount()
	list := make([]model.Contact, 0, len(a.Blocked)+1)
	for _, c := range a.Blocked {
		if c.ID != id {
			list = append(list, c)
		}
	}
	if blocked {
		name := id
		for _, c := range b.chats {
			if c.ID == id {
				name = c.Name
			}
		}
		list = append(list, model.Contact{ID: id, Name: name})
	}
	a.Blocked = list
	b.emit(model.AccountEvent{})
}
