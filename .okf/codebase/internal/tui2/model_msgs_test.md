---
description: Source module internal/tui2/model_msgs_test.go (102 lines).
resource: internal/tui2/model_msgs_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: model_msgs_test.go
type: Module
---

# Module model_msgs_test.go

**Path**: `internal/tui2/model_msgs_test.go`  
**Lines**: 102

## Snippet Preview

```
package tui2

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
)

func TestChatEventLoopRearmsAfterEachEvent(t *testing.T) {
	bus := eventbus.New()
	defer bus.Close()

	pub := eventbus.Publish[tauchat.ChatEvent](bus.Client("pub"))
	sub := eventbus.Subscribe[tauchat.ChatEvent](bus.Client("sub"))
	defer sub.Close()

	rt := &fakeRuntime{}
	m := newTestModel(rt, sub)

	pub.Publish(tauchat.ChatNotificationEvent{Message: "one"})
	pub.Publish(tauchat.ChatNotificationEvent{Message: "two"})
	pub.Publish(tauchat.ChatNotificationEvent{Message: "three"})

	var delivered []string
	cmd := m.Init()
	for i := 0; i < 3 && len(delivered) < 3; i++ {
```
