'use client';

import { useRef } from 'react';
import {
  IconArrowUp,
  IconFile,
  IconMicrophone,
  IconPaperclip,
  IconPlayerStopFilled,
  IconSlash,
} from '@tabler/icons-react';
import {
  ActionIcon,
  Box,
  CloseButton,
  Divider,
  Group,
  Image,
  Paper,
  Stack,
  Text,
  TextInput,
  Tooltip,
} from '@mantine/core';
import { useChatContext } from './Chat.context';
import { ACCEPTED_MIME_TYPES, validateFiles } from './Chat.files';
import { VoiceRecordingUI } from './VoiceRecordingUI';
import classes from './Chat.module.css';

export function ChatInput() {
  const { state, actions, meta } = useChatContext();
  const inputRef = useRef<HTMLInputElement>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const handleFileSelect = (e: React.ChangeEvent<HTMLInputElement>) => {
    if (e.target.files) {
      const valid = validateFiles(Array.from(e.target.files));
      if (valid.length > 0) {
        actions.addFiles(valid);
      }
    }
    e.target.value = '';
  };

  return (
    <Box className={classes.inputArea} p="xs" onClick={() => inputRef.current?.focus()}>
      <input
        ref={fileInputRef}
        type="file"
        accept={ACCEPTED_MIME_TYPES.join(',')}
        multiple
        hidden
        onChange={handleFileSelect}
      />

      <Stack gap="sm">
        {state.files.length > 0 && (
          <Group gap="xs" wrap="wrap">
            {state.files.map((file) => (
              <Paper key={file.id} withBorder radius="md" p={4} pos="relative">
                <CloseButton
                  size="xs"
                  pos="absolute"
                  top={-8}
                  right={-8}
                  bg="var(--mantine-color-body)"
                  bd="1px solid var(--mantine-color-default-border)"
                  radius="xl"
                  onClick={() => actions.removeFile(file.id)}
                  style={{ zIndex: 1 }}
                />
                {file.previewUrl ? (
                  <Image
                    src={file.previewUrl}
                    alt={file.name}
                    w={60}
                    h={60}
                    fit="cover"
                    radius="sm"
                  />
                ) : (
                  <Group gap={4} px="xs" h={40} wrap="nowrap">
                    <IconFile size={14} />
                    <Text size="xs" maw={80} truncate>
                      {file.name}
                    </Text>
                  </Group>
                )}
              </Paper>
            ))}
          </Group>
        )}

        {meta.voice?.isRecording ? (
          <Group gap="sm" align="center" wrap="nowrap">
            <VoiceRecordingUI
              transcript={meta.voice.transcript}
              analyser={meta.voice.analyser}
              waveformMode={meta.voice.waveformMode}
              onToggleWaveformMode={meta.voice.toggleWaveformMode}
              onStop={meta.voice.stop}
            />
          </Group>
        ) : (
          <form
            onSubmit={(e) => {
              e.preventDefault();
              if (state.isLoading) {
                return;
              }
              actions.submit();
              requestAnimationFrame(() => inputRef.current?.focus());
            }}
          >
            <Box className={classes.inputBox}>
              <TextInput
                ref={inputRef}
                value={state.input}
                onChange={(e) => actions.setInput(e.currentTarget.value)}
                placeholder={state.isLoading ? 'Queue another message...' : 'Send a message...'}
                variant="unstyled"
                px="md"
                size="sm"
                pt="xss"
              />
              <Divider />
              <Group gap="xs" justify="flex-end" px="xs" py="2xs">
                <Tooltip label="Attach files">
                  <ActionIcon
                    variant="subtle"
                    size="md"
                    radius="xl"
                    onClick={() => fileInputRef.current?.click()}
                    aria-label="Attach files"
                  >
                    <IconPaperclip size={16} />
                  </ActionIcon>
                </Tooltip>
                <Tooltip label="Commands">
                  <ActionIcon variant="subtle" size="md" radius="xl" aria-label="Commands">
                    <IconSlash size={16} />
                  </ActionIcon>
                </Tooltip>
                <Tooltip label="Voice input">
                  <ActionIcon
                    variant="subtle"
                    size="md"
                    radius="xl"
                    onClick={meta.voice?.start}
                    disabled={!meta.voice?.isSupported || state.isLoading}
                    aria-label="Voice input"
                  >
                    <IconMicrophone size={16} />
                  </ActionIcon>
                </Tooltip>
                {state.isLoading ? (
                  <ActionIcon
                    variant="filled"
                    size="md"
                    radius="md"
                    onClick={actions.stop}
                    aria-label="Stop response"
                  >
                    <IconPlayerStopFilled size={16} />
                  </ActionIcon>
                ) : (
                  <ActionIcon
                    type="submit"
                    variant="light"
                    size="md"
                    disabled={!meta.canSubmit}
                    aria-label="Send message"
                  >
                    <IconArrowUp size={16} />
                  </ActionIcon>
                )}
              </Group>
            </Box>
          </form>
        )}
      </Stack>
    </Box>
  );
}
