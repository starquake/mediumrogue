package server

// RequireSameOriginPostsForTest re-exports the cross-origin POST guard
// (middleware.go) so the external test package can drive it directly with a
// stub next handler, without booting the whole handler tree.
var RequireSameOriginPostsForTest = requireSameOriginPosts

// IntentErrorStatusForTest re-exports handleIntent's error→status mapping
// (api.go) so a test can drive every game sentinel through it — Deps.World is
// a concrete *game.World, so no stub can make SubmitIntent return an
// arbitrary error. Returns the status and whether the error was recognized.
var IntentErrorStatusForTest = intentErrorStatus
