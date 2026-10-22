import { useState } from "react";
import { IconButton, Tooltip } from "@/components/ui";
import { Icon } from "@/components/icons";
import { applyTheme, readTheme, type Theme } from "@/lib/theme";

const themeLabels: Record<Theme, string> = { system: "Auto", light: "Light", dark: "Dark" };

/**
 * The light and dark switch: Auto, Light, Dark in turn. It says its word to
 * screen readers and to the tests, which read the button's text.
 */
export function ThemeButton({ side }: { side?: "top" | "bottom" | "right" }) {
  const [theme, setTheme] = useState<Theme>(readTheme);
  function cycle() {
    const next: Theme = theme === "system" ? "light" : theme === "light" ? "dark" : "system";
    setTheme(next);
    applyTheme(next);
  }
  const glyph = theme === "system" ? <Icon.Monitor /> : theme === "light" ? <Icon.Sun /> : <Icon.Moon />;
  return (
    <Tooltip text={`Theme: ${themeLabels[theme]}`} side={side}>
      <IconButton icon={glyph} label={themeLabels[theme]} size="sm" onClick={cycle} data-action="theme" data-guide="theme" />
    </Tooltip>
  );
}
