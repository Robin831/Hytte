package auth

import "strings"

// UnknownDeviceLabel is returned by DeviceLabel when nothing recognisable can be
// derived from the User-Agent. The frontend matches on this exact value to show a
// localized placeholder instead.
const UnknownDeviceLabel = "Unknown device"

// DeviceLabel derives a short, human-friendly description of the device behind a
// User-Agent string, e.g. "Chrome on Windows" or "Safari on iPhone".
//
// It is deliberately dependency-free and best-effort: User-Agent strings are
// spoofable and full of legacy tokens, so the goal is only to help a user tell
// their own sessions apart. Anything unrecognised becomes UnknownDeviceLabel.
func DeviceLabel(ua string) string {
	ua = strings.TrimSpace(ua)
	if ua == "" {
		return UnknownDeviceLabel
	}

	browser := browserName(ua)
	platform := platformName(ua)

	switch {
	case browser != "" && platform != "":
		return browser + " on " + platform
	case browser != "":
		return browser
	case platform != "":
		return platform
	default:
		return UnknownDeviceLabel
	}
}

// browserName picks the browser out of a User-Agent. Order matters: Chromium
// forks all carry "Chrome" (and often "Safari") in their UA, so the more
// specific tokens must be tested first.
func browserName(ua string) string {
	switch {
	case strings.Contains(ua, "Edg/"), strings.Contains(ua, "EdgA/"), strings.Contains(ua, "EdgiOS/"), strings.Contains(ua, "Edge/"):
		return "Edge"
	case strings.Contains(ua, "OPR/"), strings.Contains(ua, "Opera"):
		return "Opera"
	case strings.Contains(ua, "SamsungBrowser/"):
		return "Samsung Internet"
	case strings.Contains(ua, "Firefox/"), strings.Contains(ua, "FxiOS/"):
		return "Firefox"
	case strings.Contains(ua, "CriOS/"), strings.Contains(ua, "Chrome/"), strings.Contains(ua, "Chromium/"):
		return "Chrome"
	// Safari only claims the name when it also reports a Version/ token; the
	// bare "Safari/" suffix is shared by every WebKit-derived UA.
	case strings.Contains(ua, "Safari/") && strings.Contains(ua, "Version/"):
		return "Safari"
	case strings.HasPrefix(ua, "curl/"):
		return "curl"
	case strings.HasPrefix(ua, "Wget/"):
		return "Wget"
	case containsFold(ua, "bot"), containsFold(ua, "crawler"), containsFold(ua, "spider"):
		return "Bot"
	default:
		return ""
	}
}

// platformName picks the operating system or device out of a User-Agent.
func platformName(ua string) string {
	switch {
	case strings.Contains(ua, "iPhone"):
		return "iPhone"
	case strings.Contains(ua, "iPad"):
		return "iPad"
	case strings.Contains(ua, "iPod"):
		return "iPod"
	case strings.Contains(ua, "Android"):
		return "Android"
	case strings.Contains(ua, "CrOS"):
		return "ChromeOS"
	case strings.Contains(ua, "Windows"):
		return "Windows"
	case strings.Contains(ua, "Macintosh"), strings.Contains(ua, "Mac OS X"):
		return "macOS"
	case strings.Contains(ua, "Linux"), strings.Contains(ua, "X11"):
		return "Linux"
	default:
		return ""
	}
}

// containsFold reports whether needle (already lowercase) occurs in s,
// ignoring case.
func containsFold(s, needle string) bool {
	return strings.Contains(strings.ToLower(s), needle)
}
