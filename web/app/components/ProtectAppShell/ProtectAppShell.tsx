'use client';

import { type ReactNode } from 'react';
import { OrganizationSwitcher, Protect, SignedIn, useClerk, UserButton } from '@clerk/nextjs';
import { fetchServerSentEvents } from '@tanstack/ai-client';
import { AppShell, Burger, Button, Center, Group, Stack, Text, Title } from '@mantine/core';
import { useDisclosure } from '@mantine/hooks';
import { useClientTools } from '@/ai/tools/useClientTools';
import { Chat } from '@/components/Chat/Chat';
import { ColorSchemeToggle } from '@/components/ColorSchemeToggle/ColorSchemeToggle';

function ProtectAppShellFallback() {
  const { openSignIn, openSignUp } = useClerk();
  return (
    <Center mih="100vh">
      <Stack align="center" gap="lg">
        <Title order={2}>Welcome to Pivox</Title>
        <Text c="dimmed" ta="center" maw={400}>
          Sign in to access your dashboard, or create an account to get started.
        </Text>
        <Group>
          <Button variant="filled" onClick={() => openSignIn()}>
            Sign In
          </Button>
          <Button variant="light" onClick={() => openSignUp()}>
            Sign Up
          </Button>
        </Group>
      </Stack>
    </Center>
  );
}
export default function ProtectAppShell({ children }: { children: ReactNode }) {
  const [opened, { toggle }] = useDisclosure();
  const { tools, toolParts } = useClientTools();

  return (
    <Protect fallback={<ProtectAppShellFallback />}>
      <AppShell
        header={{ height: 60 }}
        footer={{ height: 20 }}
        navbar={{ width: 300, breakpoint: 'sm', collapsed: { mobile: !opened } }}
        aside={{ width: 400, breakpoint: 'md', collapsed: { desktop: false, mobile: true } }}
        padding="md"
      >
        <AppShell.Header>
          <Group h="100%" px="md" justify="space-between">
            <Group>
              <Burger opened={opened} onClick={toggle} hiddenFrom="sm" size="sm" />
              Header
            </Group>
            <Group>
              <ColorSchemeToggle />
              <SignedIn>
                <UserButton />
              </SignedIn>
            </Group>
          </Group>
        </AppShell.Header>
        <AppShell.Navbar p="md">
          <OrganizationSwitcher />
        </AppShell.Navbar>
        <AppShell.Main>{children}</AppShell.Main>
        <AppShell.Aside p="0">
          {' '}
          <Chat.Provider connection={fetchServerSentEvents('/api/chat')} tools={tools}>
            <Chat.Root>
              <Chat.Header />
              <Chat.EmptyState />
              <Chat.MessageList
                parts={{ ...Chat.defaultParts }}
                toolParts={{ ...Chat.defaultToolParts, ...toolParts }}
              />
              <Chat.ErrorAlert />
              <Chat.Input />
            </Chat.Root>
          </Chat.Provider>
        </AppShell.Aside>
        <AppShell.Footer px="sm" py="0" fz="xs" fw="bolder">
          Footer
        </AppShell.Footer>
      </AppShell>
    </Protect>
  );
}
