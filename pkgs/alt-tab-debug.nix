{
  alt-tab-macos,
}:

alt-tab-macos.overrideAttrs (old: {
  pname = "alt-tab-debug";
  patches = (old.patches or [ ]) ++ [ ./alt-tab-debug-defaults.patch ];

  # Upstream exposes its local Pro test mode only in DEBUG builds.
  buildPhase =
    builtins.replaceStrings
      [ "-emit-executable -module-name AltTab" ]
      [ "-D DEBUG -emit-executable -module-name AltTab" ]
      old.buildPhase;

  postPatch = (old.postPatch or "") + ''
    # Keep DEBUG's diagnostics available without opening QA at launch, and use
    # upstream's mock Pro state for every invocation of this dedicated build.
    substituteInPlace src/App.swift \
      --replace-fail 'QAMenu.shared?.orderFront(nil)' '// QA menu opens only when requested.' \
      --replace-fail 'if CommandLine.arguments.contains("--mock-pro")' 'if true'

    substituteInPlace src/preferences/Preferences.swift \
      --replace-fail \
        '    static func initialize() {' \
        '    static func initialize() {
        seedDedicatedBuildDefaultsIfNeeded()' \
      --replace-fail \
        '    static func resetAll() {' \
        '    private static func seedDedicatedBuildDefaultsIfNeeded() {
        let marker = "dedicatedDefaultsVersion"
        let defaults = UserDefaults.standard
        let domain = defaults.persistentDomain(forName: App.bundleIdentifier) ?? [:]
        guard (domain[marker] as? Int ?? 0) < 2 else { return }
        for index in [2, 3] {
            let key = indexToName("shortcutStyleOverride", index)
            if domain[key] == nil {
                defaults.set(ShortcutStylePreference.focusOnRelease.indexAsString, forKey: key)
            }
            let previewKey = indexToName("previewFocusedWindowOverride", index)
            if domain[previewKey] == nil {
                defaults.set(true, forKey: previewKey)
            }
        }
        defaults.set(2, forKey: marker)
        invalidateAllCache()
    }

    static func resetAll() {'

  '';

  meta = old.meta // {
    description = "AltTab DEBUG build with upstream mock Pro mode enabled";
  };
})
