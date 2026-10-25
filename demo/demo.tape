# Records demo/demo.gif: vhs demo/demo.tape, from the repository root,
# with lazychanges, ttyd and ffmpeg on the PATH.
Output demo/demo.gif
Require lazychanges

Set Shell "bash"
Set FontSize 15
Set Width 1280
Set Height 440
Set Padding 14
Set Theme "Catppuccin Mocha"
Set TypingSpeed 60ms

Hide
Type "demo/setup.sh /tmp/lazychanges-demo && (demo/agent.sh /tmp/lazychanges-demo &) && cd /tmp/lazychanges-demo && clear"
Enter
Sleep 300ms
Show

Type "lazychanges"
Sleep 400ms
Enter
Sleep 4.5s
# The agent added refunds.go: it shows up marked as just changed.
Type "j"
Sleep 3s
# Reviewed: the cursor moves on to the next file left to read.
Type "v"
Sleep 3.5s
# The agent is done; fold and unfold the api repository.
Type@250ms "kkkkkkk"
Sleep 1s
Type "h"
Sleep 2.5s
Type "l"
Sleep 2.5s
