import type { LucideIcon } from "lucide-react";

// Decorative icons inherit the label's color; text remains the accessible name.
export function Icon({
  icon: Glyph,
  size = 16,
  className = "",
}: {
  icon: LucideIcon;
  size?: number;
  className?: string;
}) {
  return (
    <Glyph
      className={`app-icon ${className}`}
      size={size}
      strokeWidth={1.75}
      aria-hidden="true"
      focusable="false"
    />
  );
}
