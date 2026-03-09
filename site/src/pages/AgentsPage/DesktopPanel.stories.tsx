import type { Meta, StoryObj } from "@storybook/react-vite";
import { DesktopPanel } from "./DesktopPanel";
import { mockAttach, mockDesktopConnection } from "./desktopStoryUtils";

const meta: Meta<typeof DesktopPanel> = {
	title: "pages/AgentsPage/DesktopPanel",
	component: DesktopPanel,
	args: {
		isExpanded: false,
	},
	decorators: [
		(Story) => (
			<div style={{ height: 400, width: 480, border: "1px solid #333" }}>
				<Story />
			</div>
		),
	],
};
export default meta;
type Story = StoryObj<typeof DesktopPanel>;

export const Connecting: Story = {
	args: {
		desktopConnection: mockDesktopConnection({ status: "connecting" }),
	},
};

export const Connected: Story = {
	args: {
		desktopConnection: mockDesktopConnection({
			status: "connected",
			hasConnected: true,
			attach: mockAttach(),
		}),
	},
};

export const Disconnected: Story = {
	args: {
		desktopConnection: mockDesktopConnection({
			status: "disconnected",
			hasConnected: true,
		}),
	},
};

export const ErrorState: Story = {
	args: {
		desktopConnection: mockDesktopConnection({
			status: "error",
			hasConnected: false,
		}),
	},
};

export const Idle: Story = {
	args: {
		desktopConnection: mockDesktopConnection({ status: "idle" }),
	},
};
