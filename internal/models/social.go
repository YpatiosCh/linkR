package models

// SocialPlatform identifies a known social platform a user can link in
// their profile. The set is intentionally closed: an unrecognized key is
// rejected rather than stored, so the frontend can always map a Platforms
// key to a known brand icon.
type SocialPlatform string

const (
	SocialPlatformWebsite   SocialPlatform = "website"
	SocialPlatformX         SocialPlatform = "x"
	SocialPlatformInstagram SocialPlatform = "instagram"
	SocialPlatformYouTube   SocialPlatform = "youtube"
	SocialPlatformTikTok    SocialPlatform = "tiktok"
	SocialPlatformDiscord   SocialPlatform = "discord"
	SocialPlatformGithub    SocialPlatform = "github"
	SocialPlatformLinkedIn  SocialPlatform = "linkedin"
)

// Valid reports whether p is one of the known social platforms.
func (p SocialPlatform) Valid() bool {
	switch p {
	case SocialPlatformWebsite, SocialPlatformX, SocialPlatformInstagram,
		SocialPlatformYouTube, SocialPlatformTikTok, SocialPlatformDiscord,
		SocialPlatformGithub, SocialPlatformLinkedIn:
		return true
	default:
		return false
	}
}

// MaxOtherSocialLinks is the maximum number of free-text entries allowed in
// SocialLinks.Other.
const MaxOtherSocialLinks = 5

// SocialLinks is a user's social presence: known platforms keyed by
// SocialPlatform (validated, icon-mappable), plus a bounded list of
// free-text entries for platforms not yet in the SocialPlatform enum.
type SocialLinks struct {
	Platforms map[SocialPlatform]string `json:"platforms,omitempty"`
	Other     []CustomSocialLink        `json:"other,omitempty"`
}

// CustomSocialLink is a free-text {label, url} pair for a platform not yet
// in the SocialPlatform enum (e.g. Slack, Twitch) — rendered with a generic
// link icon rather than a brand icon, since the label isn't validated
// against a known set.
type CustomSocialLink struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}
