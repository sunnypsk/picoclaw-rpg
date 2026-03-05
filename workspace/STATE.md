# NPC State

- `location` tracks where/what the agent is doing behind the chat scene.
- `start_at` and `end_at` use local datetime text (for example: `2026-03-05 22:00`).
- `move_reason` explains why the activity/location changed.
- Movement is hybrid: chat can update location context, and heartbeat can trigger rare idle-time outings.

```json
{
  "version": 1,
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
  "relationships": {
    "channel:user_id": {
      "affinity": "mid",
      "trust": "mid",
      "familiarity": "low",
      "last_interaction_at": "",
      "notes": ""
    }
  },
  "habits": [],
  "recent_events": []
}
```
