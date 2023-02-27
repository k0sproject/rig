begin {
  class Stat {
    [int]$size
    [int]$mode
    [int]$unixMode
    [int]$modTime
    [bool]$isDir
    [string]$name
    Stat([System.IO.FileSystemInfo]$fi) {
      if ($fi.Exists -eq $false) {
        throw ("file not found")
      }
      $this.isDir = ($fi.Attributes -band [System.IO.FileAttributes]::Directory)
      $this.modTime = [int](Get-Date ($fi.LastWriteTimeUtc).ToUniversalTime() -UFormat %s)
      $this.size = [int]$fi.Length
      $this.unixMode = [int]$fi.UnixFileMode
      $this.mode = [int]$fi.Attributes
      $this.name = $fi.FullName
    }
  }

  # returns FileInfo or DirectoryInfo for the given path depending on whether it is a file or a directory
  function Get-FSInfo($path) {
    try {
      $path = Resolve-Path $Path
    } catch {
      if (![System.IO.Path]::IsPathRooted($path)) {
        $path = Join-Path $pwd $path
      }
    }

    if (Test-Path $path -PathType Container) {
        return (New-Object System.IO.DirectoryInfo($Path))
    }
    return (New-Object System.IO.FileInfo($Path))
  }

  # throws when a file isn't open
  function Check-Open($f) {
    if ($f -eq $null) {
      throw "file not open"
    }
  }

  # converts an object to json, writes it to the stream, and sends a zero byte to mark the end
  function Write-JSON($stream, $obj) {
    $json = ConvertTo-Json -InputObject $obj -Depth 10 -Compress
    $bytes = [System.Text.Encoding]::UTF8.GetBytes($json)
    $stream.Write($bytes, 0, $bytes.Length)
    $stream.WriteByte(0)
  }

	$bufferSize = 32768
  
  $DebugPreference = "Continue"
  $ErrorActionPreference = "Stop"
  $ProgressPreference = "SilentlyContinue"
  
  $position = 0
  $eof = $false
  $file = $null

  # import the GetStdHandle function from kernel32.dll to get a raw stdin handle
  # it seems this kills the powershell window when the script is done, but that
  # is acceptable as this is only run in a dedicated shell
  $MethodDefinitions = @'
[DllImport("kernel32.dll", SetLastError = true)]
public static extern IntPtr GetStdHandle(int nStdHandle);
'@
  $Kernel32 = Add-Type -MemberDefinition $MethodDefinitions -Name 'Kernel32' -Namespace 'Win32' -PassThru
  $stdinHandle = $Kernel32::GetStdHandle(-10)
  $stdin = New-Object System.IO.FileStream $stdinHandle, ([System.IO.FileAccess]::Read), ([System.IO.FileShare]::Read), 16384, $false
  $stdinStream = New-Object System.IO.StreamReader $stdin

  # get a raw stdout handle
  [Console]::OutputEncoding = [System.Text.Encoding]::ASCII
  $stdout = [System.Console]::OpenStandardOutput()

  $buf = New-Object byte[] $bufferSize

  while(!$stdinStream.EndOfStream) {
    try {
      $command = $stdinStream.ReadLine()
      $parts = $command -split " "

      switch ($parts[0]) {
        'stat' {
          $path = $parts[1..($parts.Length-1)] -join " "
          try {
            $fi = Get-FSInfo $path
          } catch {
            Write-JSON $stdout @{"error" = "get-fsinfo: $($_.Exception.Message)"}
            continue
          }
          $info = New-Object Stat $fi
          $output = @{
            stat = $info
          }
          Write-JSON $stdout $output
        }
        'sum' {
          $path = $parts[1..($parts.Length-1)] -join " "
          $fi = Get-FSInfo $path
          $sum = (Get-FileHash $fi.FullName -Algorithm SHA256).Hash.ToLower()
          $props = @{
            sha256 = $sum
          }
          $output = @{
            sum = $props
          }
          Write-JSON $stdout $output
        }
        'dir' {
          $path = $parts[1..($parts.Length-1)] -join " "
          $di = Get-FSInfo $path
          if (!$di.Exists) {
            throw "directory not found"
          }
          try {
            $di.GetAccessControl() | Out-Null
          } catch {
            throw "access denied"
          }
          if ($di.GetType().Name -ne "DirectoryInfo") {
            throw "not a directory"
          }
          $infos = @()
          $di.GetFileSystemInfos() | ForEach-Object {
            $info = New-Object Stat $_
            $infos += $info
          }
          $output = @{
            dir = $infos
          }
          Write-JSON $stdout $output
        }
        # command "o" = open a file
        # second parameter is the mode (ro = readonly, c = create/truncate, a = create/append, rw = read/write)
        # last parameter is the path
        'o' { 
          if ($file -ne $null) {
            throw "file already open"
          }
          $mode = $parts[1]
          $path = $parts[2..($parts.Length-1)] -join " "
          try {
            $fi = Get-FSInfo $path
          } catch {
            Write-JSON $stdout @{"error" = "get-fsinfo: $($_.Exception.Message)"}
            continue
          }
          $path = $fi.FullName

          $fmode = $null
          switch ($mode) {
            'ro' {
              $fmode = [System.IO.FileMode]::Open
            }
            'w' {
              $fmode = [System.IO.FileMode]::CreateNew
            }
            'rw' {
              $fmode = [System.IO.FileMode]::ReadWrite
            }
            'c' {
              if ($fi.Exists) {
                $fmode = [System.IO.FileMode]::Truncate
              } else {
                $fmode = [System.IO.FileMode]::Create
              }
            }
            'a' {
              $fmode = [System.IO.FileMode]::Append
            }
            default {
              throw "invalid mode"
            }
          }

          $file = New-Object System.IO.FileStream($path, $fmode)
          $position = $file.Position
          $eof = ($file.Length -eq $position)

          $props = @{
            size = $file.Length
          }
          $output = @{
             stat = $props
          }
          Write-JSON $stdout $output
        }
        # command "seek" = seek to a position in the file
        # the first parameter is the offset
        # the second parameter is the origin (0 = start, 1 = current, 2 = end)
        'seek' {
          Check-Open $file
          $pos = [int]$parts[1]
          $whence = [int]$parts[2]
          switch ($whence) {
            0 { $position = $pos }
            1 { $position += $pos }
            2 { $position = $file.Length - [Math]::Abs($pos) }
            default {
              throw "invalid whence"
            }
          }
          $file.Position = $position
          $eof = ($file.Position -eq $file.Length)
          $props = @{
            position = $position
          }
          $output = @{
             seek = $props
          }
          Write-JSON $stdout $output
        }
        # command "read" = read bytes from the file
        # the only parameter is the number of bytes to read. -1 means read until EOF
        'r' {
          Check-Open $file

          if ($eof) {
            throw "eof"
          }

          $count = [int]$parts[1]
          if ($count -eq 0) {
            throw "zero count"
          }

          if ($count -eq -1) {
            $totalbytes = $file.Length - $position
            $position = $file.Length
            $eof = $true
            $props = @{
              bytes = $totalbytes
            }
            $output = @{
               read = $props
              }
            Write-JSON $stdout $output
            $stdout.Flush()
            $file.CopyTo($stdout)
            $stdout.Flush()
            continue
          }

          if ($count -gt $bufferSize) { 
            throw ("count exceeds buffer size " + $bufferSize)
          }

          if ($file.EndOfStream) {
            $eof = $true
            throw "eof"
          }

          $bytesRead = $file.Read($buf, 0, $count)

          if ($bytesRead -eq 0) {
            $eof = $true
            throw "eof"
          }
          $position += $bytesRead
          $props = @{
            bytes = $bytesRead
          }
          $output = @{
             read = $props
          }
          Write-JSON $stdout $output
          $stdout.Flush()
          $stdout.Write($buf, 0, $bytesRead)
          $stdout.Flush()
        }
        # command "w" = write bytes to the opened file from stdin
        # the only parameter is the number of bytes to write
        'w' {
          Check-Open $file
          if (-not ($file -is [System.IO.FileStream] -and $file.CanWrite)) {
            throw "file not open for writing"
          }

          $count = [int]$parts[1]
          if ($count -eq 0) {
            throw "zero count"
          }
          $stdout.WriteByte(0)

          $bytesRead = 0
          $totalBytesRead = 0
          while ($totalBytesRead -lt $count) {
            $bytesToRead = [Math]::Min($bufferSize, $count - $totalBytesRead)
            $bytesRead = $stdin.Read($buf, 0, $bytesToRead)
            $file.Write($buf, 0, $bytesRead)
            $position = $file.Position
            $totalBytesRead += $bytesRead
          }
          $eof = $true
        }
        # command "c" = close the opened file
        'c' {
          Check-Open $file
          $file.Close()
          $file.Dispose()
          $file = $null
          $eof = $false
          $position = 0
          $stdout.WriteByte(0)
        }
        'q' {
          throw "quit"
        }
        default {
          throw "invalid command"
        }
      }
    } catch {
      if ($_.Exception.Message -eq "quit") {
        break
      }
      $output = @{
         error = $_.Exception.Message
      }
      Write-JSON $stdout $output
    }
  }
}
end {
  if ($file -ne $null) {
    $file.Close()
    $file.Dispose()
  }
}
