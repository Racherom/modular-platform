// Package ci holds the tests that check the repository's own build and release files: the
// GitHub workflows, the Taskfile, the online editor's Compose files and the scripts under
// scripts/. It is no bounded context and has no code of its own; its tests sit here so that
// libs/render stays a pure domain that reads no workflow and runs no shell or git.
package ci
