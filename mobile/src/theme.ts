// Layout borrows the keypad-first money-app model; the palette is our own.
export const colors = {
  night: '#0B0D0C', // dark chrome behind the home sheet
  nightRaised: '#1A1E1C',
  sheet: '#F3F3F0', // light surface most screens sit on
  card: '#FFFFFF',
  ink: '#0B0D0C',
  inkMuted: '#6B716C',
  line: '#E2E3DE',
  accent: '#f8e347',
  // accent: '#B6F36A',
  onAccentWash: 'rgba(11, 13, 12, 0.09)', // buttons and chips drawn on the accent
  onNightWash: 'rgba(255, 255, 255, 0.1)',
  onDayWash: 'rgba(8, 32, 84, 0.26)', // the same strip over the bright day sky
  white: '#FFFFFF',
  danger: '#D4372C',
};

export const space = { xs: 4, sm: 8, md: 16, lg: 24, xl: 40 };
export const radius = { md: 16, lg: 28, sheet: 36, pill: 999 };

// Clearance for the floating tab bar.
export const TAB_BAR_SPACE = 96;

// System font (SF Pro on iOS, Roboto on Android), kept on the light side.
export const weight = {
  regular: '400',
  medium: '500',
  semibold: '600',
} as const;
