import { execFile } from 'node:child_process';
import { platform } from 'node:os';
import { promisify } from 'node:util';

const execFileAsync = promisify(execFile);

export async function selectSystemDirectory(): Promise<string | null> {
  switch (platform()) {
    case 'darwin':
      return runDirectoryPicker('osascript', [
        '-e',
        'POSIX path of (choose folder with prompt "Choose Git Plus data location")',
      ]);
    case 'win32':
      return runDirectoryPicker('powershell.exe', [
        '-NoProfile',
        '-STA',
        '-Command',
        [
          'Add-Type -AssemblyName System.Windows.Forms;',
          '$dialog = New-Object System.Windows.Forms.FolderBrowserDialog;',
          '$dialog.Description = "Choose Git Plus data location";',
          '$dialog.ShowNewFolderButton = $true;',
          'if ($dialog.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) {',
          '  [Console]::Out.WriteLine($dialog.SelectedPath);',
          '} else { exit 1 }',
        ].join(' '),
      ]);
    default:
      return selectLinuxDirectory();
  }
}

async function selectLinuxDirectory(): Promise<string | null> {
  const candidates: Array<{ command: string; args: Array<string> }> = [
    {
      command: 'zenity',
      args: [
        '--file-selection',
        '--directory',
        '--title=Choose Git Plus data location',
      ],
    },
    {
      command: 'kdialog',
      args: ['--getexistingdirectory', '.', 'Choose Git Plus data location'],
    },
  ];

  for (const candidate of candidates) {
    try {
      return await runDirectoryPicker(candidate.command, candidate.args);
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code === 'ENOENT') continue;
      throw error;
    }
  }

  throw new Error('no supported system folder picker was found');
}

async function runDirectoryPicker(
  command: string,
  args: Array<string>,
): Promise<string | null> {
  try {
    const { stdout } = await execFileAsync(command, args, {
      timeout: 5 * 60 * 1000,
    });
    const selectedPath = stdout.trim().replace(/\/+$/, '');
    return selectedPath || null;
  } catch (error) {
    if (isPickerCancel(error)) return null;
    throw error;
  }
}

function isPickerCancel(error: unknown): boolean {
  const candidate = error as {
    code?: string | number;
    stderr?: string;
    signal?: string;
  };
  if (candidate.signal === 'SIGTERM') return true;
  if (candidate.code === 1) return true;
  if (
    typeof candidate.stderr === 'string' &&
    candidate.stderr.toLowerCase().includes('user canceled')
  ) {
    return true;
  }

  return false;
}
