import { IconChartBar, IconPlayerStop, IconWaveSine } from '@tabler/icons-react';
import { ActionIcon, Box, Group, Text, Tooltip } from '@mantine/core';
import type { WaveformMode } from './Chat.types';
import { VoiceWaveform } from './VoiceWaveform';

interface VoiceRecordingUIProps {
  transcript: string;
  analyser: AnalyserNode | null;
  waveformMode: WaveformMode;
  onToggleWaveformMode: () => void;
  onStop: () => void;
}

export function VoiceRecordingUI({
  transcript,
  analyser,
  waveformMode,
  onToggleWaveformMode,
  onStop,
}: VoiceRecordingUIProps) {
  return (
    <Group gap="sm" align="center" wrap="nowrap" flex={1}>
      {analyser && (
        <VoiceWaveform analyser={analyser} mode={waveformMode} width={120} height={36} />
      )}

      <Box flex={1} maw="100%" style={{ overflow: 'hidden' }}>
        <Text size="sm" truncate c={transcript ? undefined : 'dimmed'}>
          {transcript || 'Listening...'}
        </Text>
      </Box>

      <Group gap={4}>
        <Tooltip label={waveformMode === 'bars' ? 'Switch to wave' : 'Switch to bars'}>
          <ActionIcon
            variant="subtle"
            size="sm"
            onClick={onToggleWaveformMode}
            aria-label="Toggle waveform mode"
          >
            {waveformMode === 'bars' ? <IconWaveSine size={14} /> : <IconChartBar size={14} />}
          </ActionIcon>
        </Tooltip>

        <Tooltip label="Stop recording">
          <ActionIcon
            variant="filled"
            color="red"
            size="input-sm"
            onClick={onStop}
            aria-label="Stop recording"
          >
            <IconPlayerStop size={16} />
          </ActionIcon>
        </Tooltip>
      </Group>
    </Group>
  );
}
