# 25. Compact Token Usage / Cost Dashboard

## Status: Not yet planned

### Motivation

Users care about cost but have no visibility into cumulative spending. Tau should track total usage across sessions and show a simple dashboard.

### Design

- `~/.tau/usage.json` accumulates token counts and costs across sessions
- `/usage` command: shows current session + all-time stats
- Weekly/monthly breakdowns
- Configurable spending alerts (`max_monthly_spend: $50`)
- `tau usage` CLI command for terminal dashboard

### Output format

```shell
Usage Summary

This session:  $0.0123  |  45,230 tokens  |  12 messages
Today:         $0.0456  |  180,450 tokens  |  48 messages
This month:    $1.2345  |  4.5M tokens     |  1,200 messages
All time:      $12.3456 |  45M tokens      |  12,000 messages
```
