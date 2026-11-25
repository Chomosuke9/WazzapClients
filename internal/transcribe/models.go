package transcribe

import "runtime"

// file is something that downloads, checked by its SHA-256.
type file struct {
	url, name string
	size      int64
	sha256    string
}

// release is the whisper.cpp release the engine is.
const release = "b5907"

// engines are whisper.cpp's own builds, by GOOS/GOARCH. Its macOS one has
// no program to run.
var engines = map[string]file{
	"linux/amd64":   {name: "whisper-bin-ubuntu-x64.tar.gz", size: 10413708, sha256: "a6199027fe5b4239c84171d2bca4c0ff44ec86a515494d5a39b237b212d429bd"},
	"linux/arm64":   {name: "whisper-bin-ubuntu-arm64.tar.gz", size: 4623550, sha256: "da2e86ac058e1db0281167355428665b82c0d92e4506d186a3894bec55ceefef"},
	"windows/amd64": {name: "whisper-bin-x64.zip", size: 9168428, sha256: "ecca07b277d943e9b9f4712856a144b857271a6b333104e511e1988e80db582b"},
	"windows/arm64": {name: "whisper-bin-win-cpu-arm64.zip", size: 4379517, sha256: "4e522946827dcbc517d3cd7ee8d6cfe760e8143076924ac8513622694aa3ccfd"},
}

// engineFile is the engine for this computer.
func engineFile() (file, bool) {
	f, ok := engines[runtime.GOOS+"/"+runtime.GOARCH]
	f.url = "https://github.com/ggml-org/whisper.cpp/releases/download/" + release + "/" + f.name
	return f, ok
}

const modelURL = "https://huggingface.co/ggerganov/whisper.cpp/resolve/5359861c739e955e79d9a303bcbc70fb988958b1/"

// vad is Silero's voice detector. Whisper makes words up for silence and
// noise, so it only hears the parts with a voice.
var vad = file{
	url:  "https://huggingface.co/ggml-org/whisper-vad/resolve/9ffd54a1e1ee413ddf265af9913beaf518d1639b/ggml-silero-v6.2.0.bin",
	name: "ggml-silero-v6.2.0.bin", size: 885098, sha256: "2aa269b785eeb53a82983a20501ddf7c1d9c48e33ab63a41391ac6c9f7fb6987",
}

// Model is one of Whisper's models.
type Model struct {
	ID string
	f  file
}

// Models are the ones to choose from, all of them of every language, the
// fastest first.
var Models = []Model{
	{"base", file{name: "ggml-base-q5_1.bin", size: 59707625, sha256: "422f1ae452ade6f30a004d7e5c6a43195e4433bc370bf23fac9cc591f01a8898"}},
	{"small", file{name: "ggml-small-q5_1.bin", size: 190085487, sha256: "ae85e4a935d7a567bd102fe55afc16bb595bdb618e11b2fc7591bc08120411bb"}},
	{"turbo", file{name: "ggml-large-v3-turbo-q5_0.bin", size: 574041195, sha256: "394221709cd5ad1f40c46e6031ca61bce88931e6e088c188294c6d5a55ffa7e2"}},
}

// DefaultModel is used until another is chosen.
const DefaultModel = "small"

// modelFile is the file of the model named id, or of the default one.
func modelFile(id string) file {
	var f file
	for _, m := range Models {
		if m.ID == id || (f.name == "" && m.ID == DefaultModel) {
			f = m.f
		}
		if m.ID == id {
			break
		}
	}
	f.url = modelURL + f.name
	return f
}

// Auto has the engine detect the language of what's said.
const Auto = "auto"

// Language is a language to transcribe, by Whisper's code.
type Language struct {
	Code, Name, English string
}

// Languages are the ones to choose from: Whisper's best ones, and the
// others most spoken.
var Languages = []Language{
	{"en", "English", "English"},
	{"id", "Bahasa Indonesia", "Indonesian"},
	{"es", "Español", "Spanish"},
	{"pt", "Português", "Portuguese"},
	{"ru", "Русский", "Russian"},
	{"ar", "العربية", "Arabic"},
	{"bn", "বাংলা", "Bengali"},
	{"ca", "Català", "Catalan"},
	{"zh", "中文", "Chinese"},
	{"hr", "Hrvatski", "Croatian"},
	{"cs", "Čeština", "Czech"},
	{"da", "Dansk", "Danish"},
	{"nl", "Nederlands", "Dutch"},
	{"fi", "Suomi", "Finnish"},
	{"fr", "Français", "French"},
	{"de", "Deutsch", "German"},
	{"el", "Ελληνικά", "Greek"},
	{"he", "עברית", "Hebrew"},
	{"hi", "हिन्दी", "Hindi"},
	{"hu", "Magyar", "Hungarian"},
	{"it", "Italiano", "Italian"},
	{"ja", "日本語", "Japanese"},
	{"jw", "Basa Jawa", "Javanese"},
	{"ko", "한국어", "Korean"},
	{"ms", "Bahasa Melayu", "Malay"},
	{"no", "Norsk", "Norwegian"},
	{"fa", "فارسی", "Persian"},
	{"pl", "Polski", "Polish"},
	{"ro", "Română", "Romanian"},
	{"su", "Basa Sunda", "Sundanese"},
	{"sv", "Svenska", "Swedish"},
	{"tl", "Tagalog", "Tagalog"},
	{"ta", "தமிழ்", "Tamil"},
	{"th", "ไทย", "Thai"},
	{"tr", "Türkçe", "Turkish"},
	{"uk", "Українська", "Ukrainian"},
	{"ur", "اردو", "Urdu"},
	{"vi", "Tiếng Việt", "Vietnamese"},
}

// LanguageByCode returns the language with a code, or nil for Auto and
// unknown ones.
func LanguageByCode(code string) *Language {
	for i := range Languages {
		if Languages[i].Code == code {
			return &Languages[i]
		}
	}
	return nil
}
