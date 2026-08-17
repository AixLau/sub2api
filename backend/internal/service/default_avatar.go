package service

import "crypto/rand"

var defaultAvatarURLs = [...]string{
	"/default-avatars/ai-bear.webp",
	"/default-avatars/ai-cat.webp",
	"/default-avatars/ai-rabbit.webp",
	"/default-avatars/ai-cloud.webp",
}

// RandomDefaultAvatarURL returns one of the built-in avatars for a new user.
func RandomDefaultAvatarURL() string {
	var index [1]byte
	if _, err := rand.Read(index[:]); err != nil {
		return defaultAvatarURLs[0]
	}
	return defaultAvatarURLs[int(index[0])%len(defaultAvatarURLs)]
}
