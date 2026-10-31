// Package sdkpair is the pair of modules the render's end to end test runs (spec 0005, AC-12):
// pair-consumer calls pair-provider's greet capability through the sdk with its binding token.
// One Dockerfile builds both images, one target each, because a catalog entry cannot pick a
// command and only the sdk reads the env.
package sdkpair
