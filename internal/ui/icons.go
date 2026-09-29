package ui

import "golang.org/x/exp/shiny/materialdesign/icons"

// Material icons standing in for WhatsApp's own glyphs. Status and Channels
// have no close Material equivalent and are drawn by hand in draw.go.
var (
	icChats    = icons.CommunicationChat
	icGroup    = icons.SocialGroup
	icSettings = icons.ActionSettings
	icNewChat  = icons.ContentCreate
	icMenu     = icons.NavigationMoreVert
	icSearch   = icons.ActionSearch
	icArchive  = icons.ContentArchive
	icMuted    = icons.AVVolumeOff
	icTick     = icons.ActionDone
	icTicks    = icons.ActionDoneAll
	icClock    = icons.ActionSchedule
	icCamera   = icons.ImagePhotoCamera
	icVideo    = icons.AVVideocam
	icCall     = icons.CommunicationCall
	icAttach   = icons.ContentAdd
	icEmoji    = icons.EditorInsertEmoticon
	icMic      = icons.AVMic
	icSend     = icons.ContentSend
	icLock     = icons.ActionLock
	icLaptop   = icons.CommunicationForum
	icStarred  = icons.ToggleStarBorder
)

// doodleIcons decorate the chat wallpaper.
var doodleIcons = [][]byte{
	icons.CommunicationChatBubbleOutline, icons.ImagePhotoCamera, icons.ActionFavoriteBorder,
	icons.ImageMusicNote, icons.ToggleStarBorder, icons.CommunicationCall, icons.AVMic,
	icons.EditorInsertEmoticon, icons.SocialCake, icons.MapsLocalCafe, icons.ImageWBSunny,
	icons.ActionPets, icons.MapsDirectionsBike, icons.ImagePalette, icons.ActionHome,
	icons.SocialPublic, icons.HardwareHeadset, icons.MapsLocalFlorist,
}
