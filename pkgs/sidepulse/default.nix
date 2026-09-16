{
  lib,
  python3Packages,
  sidepulseSrc,
}:

python3Packages.buildPythonApplication rec {
  pname = "sidepulse-unwrapped";
  version = "0.1.0.dev20260817";
  pyproject = true;

  # `sidepulse-src` follows upstream main while flake.lock pins the compatible
  # revision used by this ordered patch stack.
  src = sidepulseSrc;

  patches = [
    ./../../patches/sidepulse-pr-14-opencode-t3.patch
    ./../../patches/sidepulse-pr-17-status-bar-runtime.patch
    ./../../patches/sidepulse-pr-26-lid-closed-leds.patch
    ./../../patches/sidepulse-pr-28-dnd.patch
    ./../../patches/sidepulse-pr-29-kitt.patch
    ./../../patches/sidepulse-pr-30-stuck-status.patch
    ./../../patches/sidepulse-pr-31-led-writer-helper.patch
  ];

  # WebKit is not imported and ScriptingBridge is guarded as an optional
  # feature. Quartz is needed by the virtual-device renderer. The upstream
  # metadata still declares the unused WebKit/ScriptingBridge distributions.
  dependencies = [
    python3Packages.pyobjc-framework-Cocoa
    python3Packages.pyobjc-framework-Quartz
    # Upstream added `qrcode` for the iPhone-pairing QR codes in `links.py`.
    python3Packages.qrcode
  ];

  nativeBuildInputs = [
    python3Packages.setuptools
    python3Packages.setuptools-scm
  ];
  nativeCheckInputs = [
    python3Packages.pytestCheckHook
    python3Packages.pyobjc-framework-WebKit
  ];
  dontCheckRuntimeDeps = true;
  pytestFlags = [
    "--ignore=ios/SidePulse/tools/tests"
    "--ignore=tests/test_cursor_provider.py"
  ];
  disabledTests = [
    # Nix build paths exceed Darwin's AF_UNIX socket-path limit.
    "test_hook_event_server_receives_socket_message"
    "test_notification_is_nonblocking_and_contains_no_event_data"
    # These assertions target pre-stack behavior or need a writable macOS user home.
    "test_sidepulse_setup_installs_hooks_guard_and_status_bar"
    "test_status_bar_launcher_uses_background_item_name"
    "test_status_bar_sync_skips_custom_device_display"
    "test_led_status_maps_agent_modes_to_programs"
    "test_write_mode_to_leds_uses_device_specific_program"
    # The headless AppKit fixture creates no status-item menu, unlike the app.
    "test_clear_agents_empties_the_monitor_and_leaves_an_idle_aggregate"
    # The tests deliberately require all PyPI metadata dependencies, including
    # optional ScriptingBridge, which nixpkgs does not package.
    "test_hard_third_party_imports_are_declared"
    "test_guarded_optional_imports_are_reachable_or_optional"
    "test_declared_dependencies_are_installed"
    "test_declared_frameworks_are_actually_installed"
    "test_every_shipped_module_imports_after_plain_install"
    "test_status_bar_imports_after_plain_install"
    "test_structured_t3_helper_operation_removes_live_session"
    "test_t3code_canonical_sessions_replace_duplicate_hook_rows"
    "test_t3code_origin_hooks_stay_pending_until_canonical_row"
    "test_t3code_reconcile_keeps_newer_hook_tool_state"
    "test_t3code_reconcile_leaves_hook_rows_when_no_canonical_events"
    "test_every_selector_literal_resolves"
    "test_agent_animation_profile_editor_builds"
    "test_custom_agent_animation_editor_builds"
    "test_custom_agent_animation_editor_preview_updates_and_reports_errors"
    "test_custom_animation_editor_command_c_copies_selected_program"
    "test_custom_animation_editor_installs_native_edit_shortcuts"
    "test_custom_editor_device_preview_applies_device_brightness"
    "test_edit_animation_clones_builtins_and_edits_custom_in_place"
    "test_settings_window_builds"
    "test_settings_window_is_not_visible"
    "test_settings_window_registers_its_fields"
    "test_settings_window_resizes_for_compact_animations_tab"
    "test_show_settings_window_brings_it_forward_and_starts_previews"
    "test_lid_idle_menu_item_tracks_the_setting"
    "test_sync_leds_forces_idle_while_the_lid_is_closed"
  ];

  pythonImportsCheck = [ "sidepulse" ];

  meta = {
    description = "Command-line and macOS tools for SidePulse Pro and SidePulse Dot";
    homepage = "https://sidepulse.io";
    license = lib.licenses.mit;
    mainProgram = "sidepulse";
    platforms = lib.platforms.darwin;
  };
}
