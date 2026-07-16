package server

// RequireSameOriginPostsForTest re-exports the cross-origin POST guard
// (middleware.go) so the external test package can drive it directly with a
// stub next handler, without booting the whole handler tree.
var RequireSameOriginPostsForTest = requireSameOriginPosts
