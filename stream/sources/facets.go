package sources

import (
	"fmt"
	"slices"
)

type blueskyFacet struct {
	Start int
	End   int
	URI   string
}

func renderBlueskyFacetText(text string, rawFacets []struct {
	Index struct {
		ByteStart int `json:"byteStart"`
		ByteEnd   int `json:"byteEnd"`
	} `json:"index"`
	Features []struct {
		Type string `json:"$type"`
		URI  string `json:"uri"`
	} `json:"features"`
}) string {
	if len(rawFacets) == 0 {
		return escape(text)
	}

	textBytes := []byte(text)
	facets := make([]blueskyFacet, 0, len(rawFacets))
	for _, facet := range rawFacets {
		link := ""
		for _, feature := range facet.Features {
			if feature.Type == "app.bsky.richtext.facet#link" && feature.URI != "" {
				link = feature.URI
				break
			}
		}
		if link == "" {
			continue
		}
		if facet.Index.ByteStart < 0 || facet.Index.ByteEnd > len(textBytes) || facet.Index.ByteStart >= facet.Index.ByteEnd {
			continue
		}
		facets = append(facets, blueskyFacet{
			Start: facet.Index.ByteStart,
			End:   facet.Index.ByteEnd,
			URI:   link,
		})
	}
	if len(facets) == 0 {
		return escape(text)
	}

	slices.SortFunc(facets, func(a, b blueskyFacet) int {
		return b.Start - a.Start
	})

	lastStart := len(textBytes)
	for _, facet := range facets {
		if facet.End > lastStart {
			continue
		}

		replacement := fmt.Sprintf(`<a href="%s">%s</a>`, escape(facet.URI), escape(string(textBytes[facet.Start:facet.End])))
		textBytes = slices.Concat(
			textBytes[:facet.Start],
			[]byte(replacement),
			textBytes[facet.End:],
		)
		lastStart = facet.Start
	}

	return escapeNonAnchorText(string(textBytes))
}

func escapeNonAnchorText(s string) string {
	// The replacements above already escaped the linked text/URL and inserted full anchor tags.
	// Escape only the remaining plain-text spans around those tags.
	const marker = "\x00ANCHOR\x00"
	anchors := []string{}

	for {
		start := indexAnchorStart(s)
		if start < 0 {
			break
		}
		end := indexAnchorEnd(s[start:])
		if end < 0 {
			return escape(s)
		}
		end += start
		anchors = append(anchors, s[start:end])
		s = s[:start] + marker + s[end:]
	}

	escaped := escape(s)
	for _, anchor := range anchors {
		escaped = replaceOnce(escaped, escape(marker), anchor)
	}
	return escaped
}

func indexAnchorStart(s string) int {
	for i := 0; i+2 < len(s); i++ {
		if s[i] == '<' && i+2 < len(s) && s[i+1] == 'a' && s[i+2] == ' ' {
			return i
		}
	}
	return -1
}

func indexAnchorEnd(s string) int {
	const closing = "</a>"
	for i := 0; i+len(closing) <= len(s); i++ {
		if s[i:i+len(closing)] == closing {
			return i + len(closing)
		}
	}
	return -1
}

func replaceOnce(s, old, new string) string {
	idx := -1
	for i := 0; i+len(old) <= len(s); i++ {
		if s[i:i+len(old)] == old {
			idx = i
			break
		}
	}
	if idx < 0 {
		return s
	}
	return s[:idx] + new + s[idx+len(old):]
}
