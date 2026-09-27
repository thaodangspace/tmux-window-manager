package agents

// Kind describes one recognized coding agent: its process basename (ID) and the
// human-facing name (Display) shown in the sidebar.
type Kind struct {
	ID      string
	Display string
}

// Kinds is the single source of truth for the coding-agent process basenames we
// detect. The set is identical to the original script's `^(claude|codex|pi)$`.
var Kinds = []Kind{
	{ID: "claude", Display: "Claude Code"},
	{ID: "codex", Display: "Codex"},
	{ID: "pi", Display: "pi"},
}

// kindByID indexes Kinds by process basename for O(1) membership and lookups.
var kindByID = func() map[string]Kind {
	m := make(map[string]Kind, len(Kinds))
	for _, k := range Kinds {
		m[k.ID] = k
	}
	return m
}()

// IsAgent reports whether the given process basename is a recognized coding
// agent. Matching is exact and case-sensitive, so "claude-code", "Claude", and
// "" are not agents.
func IsAgent(name string) bool {
	_, ok := kindByID[name]
	return ok
}

// DisplayName returns the human-facing name for an agent id. Unknown ids are
// returned unchanged so callers can display them as-is.
func DisplayName(id string) string {
	if k, ok := kindByID[id]; ok {
		return k.Display
	}
	return id
}
