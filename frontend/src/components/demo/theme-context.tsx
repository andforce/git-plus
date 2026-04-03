import { createContext, useContext } from 'react';

interface ThemeContextType {
  theme: {
    other?: {
      style?: string;
    };
  };
}

const ThemeContext = createContext<ThemeContextType>({
  theme: {
    other: {
      style: 'mantine',
    },
  },
});

export const useTheme = () => useContext(ThemeContext);

export const ThemeProvider = ThemeContext.Provider;
