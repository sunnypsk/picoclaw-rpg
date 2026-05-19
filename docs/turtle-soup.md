# 海龜湯 Game Mode

Picoclaw can host a text-only 海龜湯 / turtle soup game in any chat channel through
the built-in `turtle_soup` agent tool.

When the user asks to play, asks a turtle soup question, requests a hint/status,
guesses the solution, or gives up, the agent should call `turtle_soup` with the
right action. This keeps the agent in control of routing: unrelated messages can
still be answered normally instead of being captured by the game mode. The start
reply includes a public game code such as `TS-7K3P`. Users can use that code to
refer to the current game in later messages. Game question replies are limited to
`是`, `否`, `無關`, `部分是`, or `不能回答`.

Useful commands during a game:

- `提示` or `hint`: show the next curated hint.
- `狀態` or `status`: show the current public puzzle state.
- `放棄`, `揭曉`, or `答案`: reveal the hidden solution and end the game.

Example user messages the agent can handle with `turtle_soup`:

- `TS-7K3P 這跟八樓的人有關嗎？`
- `/turtle TS-7K3P status`
- `/turtle TS-7K3P hint`
- `/turtle TS-7K3P giveup`

The hidden `湯底` is stored in a private game-state directory outside the
agent-readable workspace. Normal session history, `STATE.md`, memory files, and
daily notes should contain only visible game text before the solution is
revealed.
