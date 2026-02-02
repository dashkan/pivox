import { toolDefinition } from '@tanstack/ai';
import { z } from 'zod';

// Step 1: Define the tool schema
export const getWeatherDef = toolDefinition({
  name: 'get_weather',
  description: 'Get the current weather for a location',
  inputSchema: z.object({
    location: z.string().describe('The city and state, e.g. San Francisco, CA'),
    unit: z.enum(['celsius', 'fahrenheit']).optional(),
  }),
  outputSchema: z.object({
    temperature: z.number(),
    conditions: z.string(),
    location: z.string(),
  }),
});

export const getWeatherServer = getWeatherDef.server(async ({ location, unit }) => {
  const response = await fetch(
    `https://api.weather.com/v1/current?location=${location}&unit=${unit || 'fahrenheit'}`
  );
  const data = await response.json();
  return {
    temperature: data.temperature,
    conditions: data.conditions,
    location: data.location,
  };
});
