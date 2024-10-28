# PowerShell script to run multiple instances of a Go program in separate Command Prompt windows

# Define the number of nodes
$nodeCount = 7

# Loop through the number of nodes
for ($i = 1; $i -le $nodeCount; $i++) {
    # Construct the command to run
    $command = "go run node.go $i"

    # Start the process in a new Command Prompt window
    Start-Process "cmd.exe" -ArgumentList "/K", $command
}

# Note: The '/K' switch tells cmd.exe to run the command and then remain open
