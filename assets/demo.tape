# VHS Demo Script for Surge
# Run: vhs demo.tape

Output assets/demo.gif
Output assets/demo.mp4

Set Shell "bash"
Set FontSize 16
Set Width 1920
Set Height 1080
Set FontFamily "MesloLGS NF"
Set Theme { "name": "IBM 3270 High Contrast", "black": "#000000", "red": "#ff5454", "green": "#00ff00", "yellow": "#ffff00", "blue": "#00aaff", "magenta": "#ff00ff", "cyan": "#00ffff", "white": "#ffffff", "brightBlack": "#666666", "brightRed": "#ff6666", "brightGreen": "#33ff33", "brightYellow": "#ffff66", "brightBlue": "#33ccff", "brightMagenta": "#ff66ff", "brightCyan": "#66ffff", "brightWhite": "#ffffff", "background": "#000000", "foreground": "#00ff00", "selection": "#222222", "cursor": "#00ff00" }
Set Padding 0
Set WindowBar Colorful
Set Framerate 60

# --- Launch Surge ---
Type "./surge"
Enter
Sleep 3s

# --- Add a Download ---
# Press 'a' to add download
Type "a"
Sleep 500ms

# Type a sample URL (use a small, fast file for demo)
Type "https://ash-speed.hetzner.com/100MB.bin"
Enter
Sleep 300ms

Type "https://sin-speed.hetzner.com/100MB.bin"
Enter
Sleep 300ms

# Press Enter to skip path (use default)
Enter
Sleep 300ms

# Press Enter to skip filename (use default)
Enter
Sleep 5s

# --- Show Download Progress ---
Sleep 5s
Screenshot assets/dashboard.png

# --- Open Settings ---
# Press 's' for settings
Type "s"
Sleep 1s

# Navigate through settings tabs (Right Arrow)
Right
Sleep 800ms
Screenshot assets/settings.png
Right
Sleep 800ms

# Close settings (Esc)
Escape
Sleep 4s
