# Pingo CLI showcase for Charm VHS on Debian/Linux
# Render on Linux with: vhs demo/showcase.tape
# Or from this repo with Docker: docker run --rm -v "$PWD:/vhs" -w /vhs ghcr.io/charmbracelet/vhs demo/showcase.tape

Output demo/pingo-showcase.gif

Require bash
Require ./pingo


Set Shell bash

Set FontSize 18
Set Width 900
Set Height 360
Set Padding 24
Set Framerate 60
Set PlaybackSpeed 0.5
Set Theme "Catppuccin Mocha"
Set BorderRadius 10

Type "./pingo --config"
Enter
Sleep 7s

# Walk through the interactive configuration form.
Space
Type "y"
Sleep 3s
Enter
Sleep 2s
Down
Sleep 500ms
Down
Sleep 500ms
Down
Sleep 2s
Enter
Sleep 2

Type "42"
Sleep 3s
Enter
Sleep 3s
