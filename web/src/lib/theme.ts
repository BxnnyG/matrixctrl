import { useEffect, useState } from "react";

type Theme = "light" | "dark";

function getStored(): Theme {
  const v = localStorage.getItem("matrixctrl_theme");
  if (v === "dark" || v === "light") return v;
  return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

function apply(theme: Theme) {
  document.documentElement.classList.toggle("dark", theme === "dark");
}

export function useTheme() {
  const [theme, setTheme] = useState<Theme>(() => {
    const t = getStored();
    apply(t);
    return t;
  });

  useEffect(() => {
    apply(theme);
    localStorage.setItem("matrixctrl_theme", theme);
  }, [theme]);

  const toggle = () => setTheme((t) => (t === "dark" ? "light" : "dark"));
  return { theme, toggle };
}
