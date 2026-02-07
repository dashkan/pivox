import type { Meta, StoryObj } from '@storybook/react';
import { ColorSchemeToggle } from './ColorSchemeToggle';

const meta: Meta<typeof ColorSchemeToggle> = {
  component: ColorSchemeToggle,
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj<typeof ColorSchemeToggle>;

export const Default: Story = {};
