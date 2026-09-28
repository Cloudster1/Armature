import { useEffect, useRef, useState } from "react";
import { useActiveTheme } from "@/api/themes";
import { startConstellation } from "@/lib/constellation";
import { subscribeThemePreview, themePreviewEffect } from "./ThemeLoader";

/**
 * The moving picture a theme asks for, drawn live behind the page. It sits
 * at the top of the scrolling column and stays there, under everything the
 * page draws, and takes no clicks. Nothing is drawn when the theme asks for
 * nothing, so the default look costs nothing.
 */
export function BackdropEffect() {
	const { data } = useActiveTheme();
	const [preview, setPreview] = useState<string | null | undefined>(
		themePreviewEffect(),
	);
	useEffect(
		() => subscribeThemePreview(() => setPreview(themePreviewEffect())),
		[],
	);
	const effect =
		preview !== undefined ? preview : (data?.theme?.spec.effect ?? null);
	if (effect !== "constellation") return null;
	return <Constellation />;
}

function Constellation() {
	const canvas = useRef<HTMLCanvasElement>(null);
	useEffect(() => {
		if (!canvas.current) return;
		return startConstellation(canvas.current);
	}, []);
	return (
		<div
			className="pointer-events-none sticky top-0 -z-10 h-0"
			data-backdrop-effect="constellation"
			aria-hidden="true"
		>
			<canvas ref={canvas} className="absolute left-0 top-0 block" />
		</div>
	);
}
