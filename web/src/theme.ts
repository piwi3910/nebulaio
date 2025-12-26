import { createTheme, MantineColorsTuple } from '@mantine/core';

const nebulaPrimary: MantineColorsTuple = [
  '#e5f4ff',
  '#cde2ff',
  '#9ac2ff',
  '#64a0ff',
  '#3883fe',
  '#1d70fe',
  '#0967ff',
  '#0056e4',
  '#004ccc',
  '#0041b4',
];

export const theme = createTheme({
  primaryColor: 'nebula',
  colors: {
    nebula: nebulaPrimary,
  },
  fontFamily: '-apple-system, BlinkMacSystemFont, Segoe UI, Roboto, Helvetica, Arial, sans-serif',
  headings: {
    fontFamily: '-apple-system, BlinkMacSystemFont, Segoe UI, Roboto, Helvetica, Arial, sans-serif',
  },
  defaultRadius: 'md',
});
