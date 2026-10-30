# mapfile testdata

| File | Nature |
| --- | --- |
| `known_good/one_cube.map` | NetRadiant-authored cube; the source of the canonical box face order in `geometry.go` |
| `one_room.map` | Generator output (`MakeRoomShell(256, 256, 128)` plus a player start and light) |
| `door_trigger.map` | Generator output |
| `train_path.map` | Generator output |
| `q2/basic_q2.map` | Synthetic Quake 2 fixture: classic dialect, face tails, unknown keys |
| `q2/valve220_door.map` | Synthetic Quake 2 fixture authored for AUE: Valve 220 axes with face tails and `mapversion 220` |
| `q2/classic_areaportal.map` | Synthetic Quake 2 fixture authored for AUE: classic dialect with non-zero content/flag tails |
| `golden/q2/*.map` | The writer's output for the three Quake 2 fixtures, frozen from AUE output. They differ from their sources only by the dropped leading comments; `TestWriteMatchesFrozenOutput` pins them byte for byte. |

The four Quake 1 fixtures are their own writer golden: AUE writes them back
byte for byte.
