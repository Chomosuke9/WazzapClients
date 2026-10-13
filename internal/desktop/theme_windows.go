package desktop

import "golang.org/x/sys/windows/registry"

const personalizeKey = `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`

// systemDark reads Settings > Personalization > Colors > "Choose your app
// mode", which Windows keeps as AppsUseLightTheme (missing before 1809).
func systemDark() (dark, ok bool) {
	k, err := registry.OpenKey(registry.CURRENT_USER, personalizeKey, registry.QUERY_VALUE)
	if err != nil {
		return false, false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("AppsUseLightTheme")
	if err != nil {
		return false, false
	}
	return v == 0, true
}
