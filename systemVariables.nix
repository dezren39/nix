{
  pkgs,
  ...
}:
{
  environment.variables = {
    EDITOR = "code-insiders";
    LANG = "en_US.UTF-8";
    COPILOT_MODEL = "claude-opus-4.5";
    OPENCODE_EXPERIMENTAL = "true";
    PLAYWRIGHT_BROWSERS_PATH = "${pkgs.playwright-driver.browsers}";
    PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD = "1";
  };
}
