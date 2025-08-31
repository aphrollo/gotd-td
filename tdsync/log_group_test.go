package tdsync

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/go-faster/errors"
	"github.com/gotd/td/clock"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestLogGroup(t *testing.T) {
	a := require.New(t)

	var buf bytes.Buffer
	logger := zerolog.New(&buf).With().Str("logger", "group").Logger()

	grp := NewLogGroup(context.Background(), &logger)
	grp.SetClock(clock.System)

	grp.Go("test-task", func(groupCtx context.Context) error {
		<-groupCtx.Done()
		return groupCtx.Err()
	})

	grp.Go("test-task2", func(groupCtx context.Context) error {
		<-groupCtx.Done()
		return nil
	})

	grp.Cancel()
	err := grp.Wait()
	a.True(errors.Is(err, context.Canceled))

	// Inspect logs to verify "group" is present
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	a.Greater(len(lines), 0)
	found := false
	for _, line := range lines {
		var entry map[string]interface{}
		a.NoError(json.Unmarshal([]byte(line), &entry))
		if loggerName, ok := entry["logger"]; ok && loggerName == "group" {
			found = true
			break
		}
	}
	a.True(found, "expected log with logger='group'")
}
