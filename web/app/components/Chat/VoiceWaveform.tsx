'use client';

import { useEffect, useRef } from 'react';
import type { WaveformMode } from './Chat.types';

interface VoiceWaveformProps {
  analyser: AnalyserNode;
  mode: WaveformMode;
  width?: number;
  height?: number;
  color?: string;
}

export function VoiceWaveform({
  analyser,
  mode,
  width = 200,
  height = 40,
  color = 'var(--mantine-primary-color-filled)',
}: VoiceWaveformProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const rafRef = useRef<number>(0);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) {
      return;
    }

    const ctx = canvas.getContext('2d');
    if (!ctx) {
      return;
    }

    // Use device pixel ratio for crisp rendering
    const dpr = window.devicePixelRatio || 1;
    canvas.width = width * dpr;
    canvas.height = height * dpr;
    ctx.scale(dpr, dpr);

    // Resolve CSS variable to actual color
    const resolvedColor =
      getComputedStyle(canvas).getPropertyValue('--waveform-color').trim() || color;

    const bufferLength = mode === 'bars' ? analyser.frequencyBinCount : analyser.fftSize;
    const dataArray = new Uint8Array(bufferLength);

    function drawBars() {
      if (!ctx) {
        return;
      }
      rafRef.current = requestAnimationFrame(drawBars);

      analyser.getByteFrequencyData(dataArray);

      ctx.clearRect(0, 0, width, height);

      // Only use lower half of frequency bins (higher bins tend to be empty for voice)
      const usableBins = Math.floor(bufferLength / 2);
      const barCount = Math.min(usableBins, 32);
      const barWidth = width / barCount;
      const gap = 1;

      for (let i = 0; i < barCount; i++) {
        const value = dataArray[i];
        const barHeight = (value / 255) * height;

        ctx.fillStyle = resolvedColor;
        ctx.fillRect(i * barWidth + gap / 2, height - barHeight, barWidth - gap, barHeight);
      }
    }

    function drawWave() {
      if (!ctx) {
        return;
      }
      rafRef.current = requestAnimationFrame(drawWave);

      analyser.getByteTimeDomainData(dataArray);

      ctx.clearRect(0, 0, width, height);
      ctx.beginPath();
      ctx.lineWidth = 2;
      ctx.strokeStyle = resolvedColor;

      const sliceWidth = width / bufferLength;
      let x = 0;

      for (let i = 0; i < bufferLength; i++) {
        const v = dataArray[i] / 128.0;
        const y = (v * height) / 2;

        if (i === 0) {
          ctx.moveTo(x, y);
        } else {
          ctx.lineTo(x, y);
        }
        x += sliceWidth;
      }

      ctx.lineTo(width, height / 2);
      ctx.stroke();
    }

    if (mode === 'bars') {
      drawBars();
    } else {
      drawWave();
    }

    return () => {
      cancelAnimationFrame(rafRef.current);
    };
  }, [analyser, mode, width, height, color]);

  return (
    <canvas
      ref={canvasRef}
      style={{
        width,
        height,
        ['--waveform-color' as string]: color,
      }}
    />
  );
}
