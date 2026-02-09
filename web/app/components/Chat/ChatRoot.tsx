'use client';

import type { ReactNode } from 'react';
import { Center, Text } from '@mantine/core';
import { Dropzone } from '@mantine/dropzone';
import { notifications } from '@mantine/notifications';
import { useChatContext } from './Chat.context';
import { ACCEPTED_MIME_TYPES, MAX_FILES, MAX_PDF_SIZE, validateFiles } from './Chat.files';
import classes from './Chat.module.css';

interface ChatRootProps {
  children: ReactNode;
}

export function ChatRoot({ children }: ChatRootProps) {
  const { actions } = useChatContext();

  const handleDrop = (dropped: File[]) => {
    const valid = validateFiles(dropped);
    if (valid.length > 0) {
      actions.addFiles(valid);
    }
  };

  return (
    <Dropzone
      className={classes.root}
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
      p={0}
      radius={0}
      h="100dvh"
      pos="relative"
      display="flex"
      style={{ flexDirection: 'column' }}
      styles={{ inner: { display: 'flex', flexDirection: 'column', flex: 1 } }}
    >
      <Dropzone.Accept>
        <Center
          pos="absolute"
          top={0}
          left={0}
          right={0}
          bottom={0}
          bg="var(--mantine-primary-color-light)"
          bd="2px dashed var(--mantine-primary-color-filled)"
          style={{ zIndex: 100 }}
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
          bd="2px dashed var(--mantine-color-red-filled)"
          style={{ zIndex: 100 }}
        >
          <Text size="sm" fw={500} c="red">
            File type not supported
          </Text>
        </Center>
      </Dropzone.Reject>

      {children}
    </Dropzone>
  );
}
