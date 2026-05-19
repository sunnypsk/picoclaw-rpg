# 海龜湯 Game Mode

Picoclaw can host a text-only 海龜湯 / turtle soup game in any chat channel.

Start a game with `/turtle`, `/turtle start`, `開一局海龜湯`, or `玩海龜湯`.
The start reply includes a public game code such as `TS-7K3P`. Users can use
that code to refer to the current game in later messages.
During a game, normal messages are treated as player questions or guesses. The
agent answers questions only with `是`, `否`, `無關`, `部分是`, or `不能回答`.

Useful commands during a game:

- `提示` or `hint`: show the next curated hint.
- `狀態` or `status`: show the current public puzzle state.
- `放棄`, `揭曉`, or `答案`: reveal the hidden solution and end the game.

Code-based examples:

- `TS-7K3P 這跟八樓的人有關嗎？`
- `/turtle TS-7K3P status`
- `/turtle TS-7K3P hint`
- `/turtle TS-7K3P giveup`

The hidden `湯底` is stored in a private game-state directory outside the
agent-readable workspace. Normal session history, `STATE.md`, memory files, and
daily notes should contain only visible game text before the solution is
revealed.
