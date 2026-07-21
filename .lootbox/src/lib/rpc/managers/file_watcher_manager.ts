/**
 * FileWatcherManager
 *
 * Manages filesystem monitoring for RPC files.
 * Handles:
 * - Watching RPC directory for file changes
 * - Debouncing rapid changes
 * - Triggering callbacks on TypeScript file modifications
 * - Lifecycle control (start/stop watching)
 */

import { DEFAULT_FILE_WATCH_DEBOUNCE_MS, DEFAULT_TOOL_FILE_EXTENSION } from "../../constants.ts";

export class FileWatcherManager {
  private watcher: Deno.FsWatcher | null = null;
  private watching = false;

  /**
   * Start watching a directory for changes
   * Calls onChange callback when TypeScript files are modified (with debouncing)
   */
  startWatching(
    directory: string,
    onChange: () => Promise<void>,
    debounceMs: number = DEFAULT_FILE_WATCH_DEBOUNCE_MS,
    fileExtension: string = DEFAULT_TOOL_FILE_EXTENSION,
  ): void {
    if (this.watching) {
      console.error("File watcher already running");
      return;
    }

    try {
      // Ensure the directory exists before trying to watch it.
      // If it was just auto-created (or doesn't exist for some reason),
      // Deno.watchFs would throw without this guard.
      try {
        const info = Deno.statSync(directory);
        if (!info.isDirectory) {
          console.error(`File watcher: ${directory} is not a directory, skipping`);
          return;
        }
      } catch {
        try {
          Deno.mkdirSync(directory, { recursive: true });
          console.error(`File watcher: created missing directory ${directory}`);
        } catch (mkdirErr) {
          console.error(`File watcher: cannot create ${directory}:`, mkdirErr);
          return;
        }
      }

      this.watcher = Deno.watchFs(directory);
      this.watching = true;

      // Start watching in background
      (async () => {
        try {
          for await (const event of this.watcher!) {
            // Only react to TypeScript file changes
            if (event.paths.some((path) => path.endsWith(fileExtension))) {
              // Debounce rapid file changes
              await new Promise((resolve) => setTimeout(resolve, debounceMs));
              await onChange();
            }
          }
        } catch (err) {
          if (this.watching) {
            // Only log if we didn't intentionally stop watching
            console.error("File watcher error:", err);
          }
        }
      })();
    } catch (err) {
      console.error("Failed to start file watcher:", err);
      this.watching = false;
    }
  }

  /**
   * Stop watching filesystem
   */
  stopWatching(): void {
    if (!this.watching) {
      return;
    }

    this.watching = false;

    if (this.watcher) {
      try {
        this.watcher.close();
      } catch {
        // Best effort — may already be closed
      }
      this.watcher = null;
    }
  }

  /**
   * Check if currently watching
   */
  isWatching(): boolean {
    return this.watching;
  }
}
