import { DEFAULT_PORT } from "../constants.ts";

async function pathExists(path: string): Promise<boolean> {
  try {
    await Deno.stat(path);
    return true;
  } catch {
    return false;
  }
}

export async function init(): Promise<void> {
  const lootboxDir = ".lootbox";
  const configFile = "lootbox.config.json";
  const subdirs = ["tools", "workflows", "scripts"] as const;

  const lootboxExists = await pathExists(lootboxDir);
  const configExists = await pathExists(configFile);

  // If both already exist and all subdirs are present, nothing to do
  if (lootboxExists && configExists) {
    const missingSubdirs = [];
    for (const sub of subdirs) {
      if (!(await pathExists(`${lootboxDir}/${sub}`))) {
        missingSubdirs.push(sub);
      }
    }

    if (missingSubdirs.length === 0) {
      console.error("Already initialized — .lootbox/ and lootbox.config.json both exist.");
      console.error("All subdirectories present (tools/, workflows/, scripts/).");
      Deno.exit(1);
    }

    // Repair: create only the missing subdirectories
    for (const sub of missingSubdirs) {
      await Deno.mkdir(`${lootboxDir}/${sub}`, { recursive: true });
      console.log(`✓ Created .lootbox/${sub}/ (was missing)`);
    }
    console.log("\nRepaired! Start server: lootbox server");
    return;
  }

  // If .lootbox exists but config doesn't, create missing subdirs + config
  if (lootboxExists) {
    for (const sub of subdirs) {
      if (!(await pathExists(`${lootboxDir}/${sub}`))) {
        await Deno.mkdir(`${lootboxDir}/${sub}`, { recursive: true });
        console.log(`✓ Created .lootbox/${sub}/ (was missing)`);
      }
    }
  } else {
    // Create full directory structure
    for (const sub of subdirs) {
      await Deno.mkdir(`${lootboxDir}/${sub}`, { recursive: true });
    }
    console.log("✓ Created .lootbox/");
    console.log("✓ Created .lootbox/tools/");
    console.log("✓ Created .lootbox/workflows/");
    console.log("✓ Created .lootbox/scripts/");
  }

  if (!configExists) {
    const defaultConfig = {
      server: {
        port: DEFAULT_PORT,
        lootboxRoot: ".lootbox",
      },
    };

    await Deno.writeTextFile(
      configFile,
      JSON.stringify(defaultConfig, null, 2) + "\n",
    );
    console.log("✓ Created lootbox.config.json");
  }

  console.log("\nReady! Start server: lootbox server");
}
