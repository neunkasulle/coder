import { Button } from "components/Button/Button";
import { Spinner } from "components/Spinner/Spinner";
import { type FC, useCallback, useEffect, useRef } from "react";
import type { UseDesktopConnectionResult } from "./useDesktopConnection";

interface DesktopPanelProps {
	desktopConnection: UseDesktopConnectionResult;
	isExpanded: boolean;
}

export const DesktopPanel: FC<DesktopPanelProps> = ({
	desktopConnection,
	isExpanded: _isExpanded,
}) => {
	const containerRef = useRef<HTMLDivElement | null>(null);

	const attachToContainer = useCallback(
		(el: HTMLDivElement | null) => {
			containerRef.current = el;
			if (el) {
				desktopConnection.attach(el);
			}
		},
		[desktopConnection],
	);

	// Re-attach when status changes to connected (e.g., after reconnect).
	useEffect(() => {
		if (desktopConnection.status === "connected" && containerRef.current) {
			desktopConnection.attach(containerRef.current);
		}
	}, [desktopConnection]);

	if (desktopConnection.status === "connecting") {
		return (
			<div className="flex h-full flex-col items-center justify-center gap-2 text-content-secondary">
				<Spinner loading className="h-6 w-6" />
				<span className="text-sm">Connecting to desktop...</span>
			</div>
		);
	}

	if (desktopConnection.status === "disconnected") {
		return (
			<div className="flex h-full flex-col items-center justify-center gap-2 text-content-secondary">
				<Spinner loading className="h-6 w-6" />
				<span className="text-sm">Desktop disconnected. Reconnecting...</span>
			</div>
		);
	}

	if (desktopConnection.status === "error") {
		return (
			<div className="flex h-full flex-col items-center justify-center gap-3 text-content-secondary">
				<span className="text-sm">
					Failed to connect to the desktop session.
				</span>
				<Button
					variant="outline"
					size="sm"
					onClick={() => desktopConnection.connect()}
				>
					Reconnect
				</Button>
			</div>
		);
	}

	if (desktopConnection.status === "idle") {
		return (
			<div className="flex h-full flex-col items-center justify-center gap-2 text-content-secondary">
				<Spinner loading className="h-6 w-6" />
				<span className="text-sm">Initializing desktop...</span>
			</div>
		);
	}

	// status === "connected"
	return <div ref={attachToContainer} className="h-full w-full" />;
};
