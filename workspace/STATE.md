# NPC State

- `location` tracks where/what the agent is doing behind the chat scene.
- `start_at` and `end_at` use RFC3339 timestamps with timezone offsets (for example: `2026-03-05T22:00:00+08:00`).
- `move_reason` explains why the activity/location changed.
- `people` stores stable person refs with human-readable display names.
- `identifier_map` stores raw channel/user identifiers only for mapping to a person ref; do not use raw IDs as readable references elsewhere in the file.
- Movement is hybrid: chat can update location context, and heartbeat can trigger rare idle-time outings.

```json
{
  "version": 2,
  "updated_at": "",
  "emotion": {
    "name": "calm",
    "intensity": "mid",
    "reason": ""
  },
  "location": {
    "area": "base",
    "scene": "workspace",
    "activity": "observing",
    "start_at": "",
    "end_at": "",
    "move_reason": ""
  },
  "people": {},
  "identifier_map": {},
  "relationships": {},
  "habits": [],
  "recent_events": []
}
```
