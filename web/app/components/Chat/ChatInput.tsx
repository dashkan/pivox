'use client';

import { useRef } from 'react';
import { IconFile, IconMicrophone, IconPaperclip, IconSend } from '@tabler/icons-react';
import {
  ActionIcon,
  Box,
  Center,
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
import { Dropzone, IMAGE_MIME_TYPE } from '@mantine/dropzone';
import { notifications } from '@mantine/notifications';
import { useChatContext } from './Chat.context';
import { VoiceRecordingUI } from './VoiceRecordingUI';

const MAX_IMAGE_SIZE = 5 * 1024 * 1024;
const MAX_PDF_SIZE = 32 * 1024 * 1024;
const MAX_TEXT_SIZE = 1 * 1024 * 1024;
const MAX_FILES = 10;

const ACCEPTED_MIME_TYPES = [
  ...IMAGE_MIME_TYPE,
  'application/pdf',
  'text/plain',
  'text/markdown',
  'text/csv',
  'application/json',
];

function validateFiles(dropped: File[]): File[] {
  const valid: File[] = [];

  for (const file of dropped) {
    if (file.type.startsWith('image/') && file.size > MAX_IMAGE_SIZE) {
      notifications.show({
        color: 'red',
        title: 'File too large',
        message: `${file.name} exceeds the 5 MB image limit`,
      });
    } else if (file.type === 'application/pdf' && file.size > MAX_PDF_SIZE) {
      notifications.show({
        color: 'red',
        title: 'File too large',
        message: `${file.name} exceeds the 32 MB PDF limit`,
      });
    } else if (
      !file.type.startsWith('image/') &&
      file.type !== 'application/pdf' &&
      file.size > MAX_TEXT_SIZE
    ) {
      notifications.show({
        color: 'red',
        title: 'File too large',
        message: `${file.name} exceeds the 1 MB text file limit`,
      });
    } else {
      valid.push(file);
    }
  }

  return valid;
}

export function ChatInput() {
  const { state, actions, meta } = useChatContext();
  const openRef = useRef<() => void>(null);

  const handleDrop = (dropped: File[]) => {
    const valid = validateFiles(dropped);
    if (valid.length > 0) {
      actions.addFiles(valid);
    }
  };

  return (
    <>
      <Divider />
      <Dropzone
        openRef={openRef}
        onDrop={handleDrop}
        onReject={(rejections) => {
          for (const rejection of rejections) {
            notifications.show({
              color: 'red',
              title: 'File rejected',
              message: `${rejection.file.name}: ${rejection.errors[0]?.message ?? 'Invalid file'}`,
            });
          }
        }}
        maxSize={MAX_PDF_SIZE}
        accept={ACCEPTED_MIME_TYPES}
        maxFiles={MAX_FILES}
        activateOnClick={false}
        enablePointerEvents
        multiple
        bd="none"
        bg="var(--mantine-color-body)"
        p={0}
        radius={0}
        pos="relative"
      >
        <Dropzone.Accept>
          <Center
            pos="absolute"
            top={0}
            left={0}
            right={0}
            bottom={0}
            bg="var(--mantine-primary-color-light)"
            style={{ zIndex: 10 }}
          >
            <Text size="sm" fw={500} c="var(--mantine-primary-color-filled)">
              Drop files here
            </Text>
          </Center>
        </Dropzone.Accept>

        <Dropzone.Reject>
          <Center
            pos="absolute"
            top={0}
            left={0}
            right={0}
            bottom={0}
            bg="var(--mantine-color-red-light)"
            style={{ zIndex: 10 }}
          >
            <Text size="sm" fw={500} c="red">
              File type not supported
            </Text>
          </Center>
        </Dropzone.Reject>

        <Box p="md">
          <Stack gap="sm" maw={768} mx="auto">
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
                  actions.submit();
                }}
              >
                <Group gap="sm" align="flex-end" wrap="nowrap">
                  <Tooltip label="Attach files">
                    <ActionIcon
                      variant="subtle"
                      size="input-sm"
                      onClick={() => openRef.current?.()}
                      aria-label="Attach files"
                    >
                      <IconPaperclip size={16} />
                    </ActionIcon>
                  </Tooltip>
                  {meta.voice?.isSupported && (
                    <Tooltip label="Voice input">
                      <ActionIcon
                        variant="subtle"
                        size="input-sm"
                        onClick={meta.voice.start}
                        disabled={state.isLoading}
                        aria-label="Voice input"
                      >
                        <IconMicrophone size={16} />
                      </ActionIcon>
                    </Tooltip>
                  )}
                  <TextInput
                    value={state.input}
                    onChange={(e) => actions.setInput(e.currentTarget.value)}
                    placeholder="Send a message..."
                    disabled={state.isLoading}
                    flex={1}
                  />
                  <ActionIcon
                    type="submit"
                    variant="filled"
                    size="input-sm"
                    disabled={!meta.canSubmit}
                    aria-label="Send message"
                  >
                    <IconSend size={16} />
                  </ActionIcon>
                </Group>
              </form>
            )}
          </Stack>
        </Box>
      </Dropzone>
    </>
  );
}
