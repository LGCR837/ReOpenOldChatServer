import React from 'react';

export enum TileSize {
  SMALL = 'small',   // 1x1
  MEDIUM = 'medium', // 2x2
  WIDE = 'wide',     // 4x2
  LARGE = 'large'    // 4x4
}

export interface TileData {
  id: string;
  size: TileSize;
  title?: string;
  count?: number | string;
  icon?: React.ReactNode;
  bgColor?: string; // Hex code or Tailwind class
  contentBack?: React.ReactNode;
  onClick?: () => void;
  delay?: number; // Animation delay for entrance
  liveEffect?: boolean; // Whether it flips
}