package agents

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
)

// claudeEvent is the subset of a Claude transcript line we care about.
type claudeEvent struct {
	Type      string         `json:"type"`
	Cwd       string         `json:"cwd"`
	IsMeta    bool           `json:"isMeta"`
	Timestamp string         `json:"timestamp"`
	Message   *claudeMessage `json:"message"`
	Model     string         `json:"model"` // top-level display name (preferred over message.model)
}

type claudeMessage struct {
	Role    string          `json:"role"`
	Model   string          `json:"model"`
	Content json.RawMessage `json:"content"`
}

// tailBytes is how much of the end of a transcript TranscriptTail reads. Claude
// lines can be long, so this is generous enough to hold several recent events.
const tailBytes = 256 * 1024

// TranscriptTail reads only the tail of a Claude transcript and returns the most
// recent assistant model and the most recent message text. A Stop hook fires
// often and only needs the freshest model/latest, so we seek near the end and
// never re-read the whole file. Missing/unreadable files yield empty strings,
// never an error.
func TranscriptTail(path string) (model, latest string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return "", ""
	}
	start := int64(0)
	if fi.Size() > tailBytes {
		start = fi.Size() - tailBytes
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return "", ""
	}

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	first := start > 0 // first line is likely partial after a mid-file seek; skip it
	var latestAt time.Time
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if first {
			first = false
			continue
		}
		if line == "" {
			continue
		}
		var ev claudeEvent
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		if ev.Message == nil {
			continue
		}
		if ev.Message.Role == "assistant" {
			// Prefer the top-level model (friendly display name) over message.model
			// (full API identifier). Both may be present; the event-level one is
			// typically the short human-readable name.
			if ev.Model != "" {
				model = ev.Model
			} else if ev.Message.Model != "" {
				model = ev.Message.Model
			}
		}
		ts := parseTimestamp(ev.Timestamp)
		if text := extractText(ev.Message.Content); text != "" && !ts.IsZero() {
			if latestAt.IsZero() || !ts.Before(latestAt) {
				latest = text
				latestAt = ts
			}
		}
	}
	return model, latest
}

var wsRe = regexp.MustCompile(`\s+`)

// extractText pulls the first non-empty text out of a message content field,
// which is either a JSON string or an array of blocks with a "text" key.
// Whitespace is collapsed, matching t2's cleanSnippet.
func extractText(content json.RawMessage) string {
	if len(content) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(content, &s) == nil {
		return clean(s)
	}
	var blocks []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(content, &blocks) == nil {
		for _, b := range blocks {
			if strings.TrimSpace(b.Text) != "" {
				return clean(b.Text)
			}
		}
	}
	return ""
}

func clean(s string) string {
	return strings.TrimSpace(wsRe.ReplaceAllString(s, " "))
}

// parseTimestamp parses the ISO-8601 timestamps Claude writes
// (e.g. "2026-06-22T16:21:27.648Z"). Returns the zero time on failure.
func parseTimestamp(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	return time.Time{}
}
