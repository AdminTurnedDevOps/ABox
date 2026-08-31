---
layout: default
title: Events
nav_order: 6
permalink: /events/
---

# Events
{: .no_toc }

`Turn` / `TurnOpts` call `onEvent` for each `agent_event` frame. Same JSON
the CLI prints in `abox exec`.

```go
sess.Turn(ctx, prompt, func(ev abox.Event) {
    switch ev.Kind {
    case "text":
        fmt.Print(ev.Text)
    case "tool":
        fmt.Printf("[%s %s]\n", ev.Tool, ev.Status)
    case "result":
        if ev.Usage != nil {
            fmt.Printf("tokens in=%d out=%d\n", ev.Usage.InputTokens, ev.Usage.OutputTokens)
        }
    case "error":
        fmt.Fprintln(os.Stderr, ev.Err)
    case "done":
        fmt.Println()
    }
})
```

## Kinds

| Kind | Fields | Notes |
| --- | --- | --- |
| `text` | `Text` | Incremental model output |
| `tool` | `Tool`, `Status`, `Text`, `Err` | `ok` or `error`. Rich: `ToolID`, `ToolArgs` |
| `result` | `Usage`, `StopReason` | Protocol 2, rich events |
| `error` | `Err` | Provider or agent failure |
| `done` | | Turn finished (CLI also uses this) |

`TurnResult` is filled from the `result` event when present, plus `Canceled`
if the guest returned `code: canceled`.

See [print-events]({{ '/examples/print-events' | relative_url }}).
