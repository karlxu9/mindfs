package api

import (
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
)

const (
	logTailDefaultLines = 200
	logTailMaxLines     = 2000
	logTailBlockSize    = 64 * 1024
	// logTailMaxLineBytes caps a single returned line so one enormous line
	// (a dumped payload, minified JSON) cannot blow up the response.
	logTailMaxLineBytes = 8 * 1024
)

type logTailResult struct {
	Path      string   `json:"path"`
	SizeBytes int64    `json:"size_bytes"`
	Lines     []string `json:"lines"`
	Truncated bool     `json:"truncated"`
}

// tailLogFile returns the last maxLines lines of the file without loading it
// whole: it reads fixed-size blocks backwards from the end until enough line
// breaks accumulated. Truncated is set when older content exists beyond the
// returned window or a single line was cut to logTailMaxLineBytes.
func tailLogFile(path string, maxLines int) (logTailResult, error) {
	result := logTailResult{Path: path, Lines: []string{}}
	file, err := os.Open(path)
	if err != nil {
		return result, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return result, err
	}
	result.SizeBytes = info.Size()
	if info.Size() == 0 || maxLines <= 0 {
		return result, nil
	}

	// Reading budget: enough for maxLines full-length lines plus slack, so a
	// file of arbitrarily long lines still gets bounded work.
	budget := int64(maxLines+1) * logTailMaxLineBytes
	start := info.Size() - budget
	if start < 0 {
		start = 0
	}

	var chunk []byte
	offset := info.Size()
	for offset > start {
		readFrom := offset - logTailBlockSize
		if readFrom < start {
			readFrom = start
		}
		block := make([]byte, offset-readFrom)
		if _, err := file.ReadAt(block, readFrom); err != nil {
			return result, err
		}
		chunk = append(block, chunk...)
		offset = readFrom
		if strings.Count(string(chunk), "\n") > maxLines {
			break
		}
	}
	if offset > 0 {
		result.Truncated = true
	}

	text := strings.TrimRight(string(chunk), "\r\n")
	if text == "" {
		return result, nil
	}
	lines := strings.Split(text, "\n")
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
		result.Truncated = true
	}
	for i, line := range lines {
		line = strings.TrimRight(line, "\r")
		if len(line) > logTailMaxLineBytes {
			line = line[:logTailMaxLineBytes] + " ...(line truncated)"
			result.Truncated = true
		}
		lines[i] = line
	}
	result.Lines = lines
	return result, nil
}

func clampLogTailLines(raw string) int {
	lines := logTailDefaultLines
	if strings.TrimSpace(raw) != "" {
		if parsed, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil {
			lines = parsed
		}
	}
	if lines < 1 {
		lines = 1
	}
	if lines > logTailMaxLines {
		lines = logTailMaxLines
	}
	return lines
}

// handleLogs serves the tail of the service log (R-4.2). Session-protected
// like every other /api/* endpoint, so it works through the relay too.
func (h *HTTPHandler) handleLogs(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(h.LogPath)
	if path == "" {
		respondError(w, http.StatusNotFound, errors.New("log file not configured for this instance"))
		return
	}
	result, err := tailLogFile(path, clampLogTailLines(r.URL.Query().Get("lines")))
	if err != nil {
		if os.IsNotExist(err) {
			respondError(w, http.StatusNotFound, errors.New("log file does not exist yet"))
			return
		}
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, result)
}
