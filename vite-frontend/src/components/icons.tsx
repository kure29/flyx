import * as React from "react";

import { IconSvgProps } from "@/types";

/**
 * The brand mark: three waves, no background (app icons in public/ put
 * them on a white tile), recoloured for dark mode. The viewBox is cropped
 * to the waves so they read at sidebar sizes.
 */
export const Logo: React.FC<IconSvgProps> = ({
  size = 36,
  height,
  ...props
}) => (
  <svg
    fill="none"
    height={size || height}
    viewBox="37 30 200 200"
    width={size || height}
    {...props}
  >
    <path
      className="fill-foreground/30"
      d="M46.0 160.0 C47.7 159.2 51.7 156.3 56.0 155.0 C60.3 153.7 66.7 152.0 72.0 152.0 C77.3 152.0 82.7 153.2 88.0 155.0 C93.3 156.8 98.7 160.2 104.0 163.0 C109.3 165.8 114.7 169.3 120.0 172.0 C125.3 174.7 130.7 177.5 136.0 179.0 C141.3 180.5 147.3 180.3 152.0 181.0 C156.7 181.7 162.0 182.7 164.0 183.0 C162.0 183.5 156.7 184.8 152.0 186.0 C147.3 187.2 141.3 189.2 136.0 190.0 C130.7 190.8 125.3 191.3 120.0 191.0 C114.7 190.7 109.3 189.8 104.0 188.0 C98.7 186.2 93.3 182.7 88.0 180.0 C82.7 177.3 77.3 174.5 72.0 172.0 C66.7 169.5 60.3 167.0 56.0 165.0 C51.7 163.0 47.7 160.8 46.0 160.0 Z"
    />
    <path
      className="fill-foreground/60"
      d="M46.0 124.0 C47.7 123.3 51.7 121.7 56.0 120.0 C60.3 118.3 66.7 115.3 72.0 114.0 C77.3 112.7 82.7 111.8 88.0 112.0 C93.3 112.2 98.7 113.5 104.0 115.0 C109.3 116.5 114.7 118.2 120.0 121.0 C125.3 123.8 130.7 128.8 136.0 132.0 C141.3 135.2 146.7 137.7 152.0 140.0 C157.3 142.3 162.7 144.8 168.0 146.0 C173.3 147.2 178.7 147.0 184.0 147.0 C189.3 147.0 195.0 146.3 200.0 146.0 C205.0 145.7 211.7 145.2 214.0 145.0 C211.7 146.5 205.0 151.3 200.0 154.0 C195.0 156.7 189.3 159.3 184.0 161.0 C178.7 162.7 173.3 163.5 168.0 164.0 C162.7 164.5 157.3 164.8 152.0 164.0 C146.7 163.2 141.3 161.5 136.0 159.0 C130.7 156.5 125.3 152.2 120.0 149.0 C114.7 145.8 109.3 142.7 104.0 140.0 C98.7 137.3 93.3 134.5 88.0 133.0 C82.7 131.5 77.3 131.7 72.0 131.0 C66.7 130.3 60.3 130.2 56.0 129.0 C51.7 127.8 47.7 124.8 46.0 124.0 Z"
    />
    <path
      className="fill-foreground"
      d="M46.0 86.0 C47.7 85.2 51.7 83.2 56.0 81.0 C60.3 78.8 66.7 75.0 72.0 73.0 C77.3 71.0 82.7 69.8 88.0 69.0 C93.3 68.2 98.7 67.7 104.0 68.0 C109.3 68.3 114.7 69.3 120.0 71.0 C125.3 72.7 130.7 75.0 136.0 78.0 C141.3 81.0 146.7 85.2 152.0 89.0 C157.3 92.8 162.7 97.7 168.0 101.0 C173.3 104.3 178.7 106.5 184.0 109.0 C189.3 111.5 194.7 114.0 200.0 116.0 C205.3 118.0 211.3 119.7 216.0 121.0 C220.7 122.3 226.0 123.5 228.0 124.0 C226.0 124.5 220.7 126.0 216.0 127.0 C211.3 128.0 205.3 129.5 200.0 130.0 C194.7 130.5 189.3 130.7 184.0 130.0 C178.7 129.3 173.3 127.8 168.0 126.0 C162.7 124.2 157.3 122.2 152.0 119.0 C146.7 115.8 141.3 110.7 136.0 107.0 C130.7 103.3 125.3 99.5 120.0 97.0 C114.7 94.5 109.3 93.3 104.0 92.0 C98.7 90.7 93.3 89.7 88.0 89.0 C82.7 88.3 77.3 88.2 72.0 88.0 C66.7 87.8 60.3 88.3 56.0 88.0 C51.7 87.7 47.7 86.3 46.0 86.0 Z"
    />
  </svg>
);

export const MoonFilledIcon = ({
  size = 24,
  width,
  height,
  ...props
}: IconSvgProps) => (
  <svg
    aria-hidden="true"
    focusable="false"
    height={size || height}
    role="presentation"
    viewBox="0 0 24 24"
    width={size || width}
    {...props}
  >
    <path
      d="M21.53 15.93c-.16-.27-.61-.69-1.73-.49a8.46 8.46 0 01-1.88.13 8.409 8.409 0 01-5.91-2.82 8.068 8.068 0 01-1.44-8.66c.44-1.01.13-1.54-.09-1.76s-.77-.55-1.83-.11a10.318 10.318 0 00-6.32 10.21 10.475 10.475 0 007.04 8.99 10 10 0 002.89.55c.16.01.32.02.48.02a10.5 10.5 0 008.47-4.27c.67-.93.49-1.519.32-1.79z"
      fill="currentColor"
    />
  </svg>
);

export const SunFilledIcon = ({
  size = 24,
  width,
  height,
  ...props
}: IconSvgProps) => (
  <svg
    aria-hidden="true"
    focusable="false"
    height={size || height}
    role="presentation"
    viewBox="0 0 24 24"
    width={size || width}
    {...props}
  >
    <g fill="currentColor">
      <path d="M19 12a7 7 0 11-7-7 7 7 0 017 7z" />
      <path d="M12 22.96a.969.969 0 01-1-.96v-.08a1 1 0 012 0 1.038 1.038 0 01-1 1.04zm7.14-2.82a1.024 1.024 0 01-.71-.29l-.13-.13a1 1 0 011.41-1.41l.13.13a1 1 0 010 1.41.984.984 0 01-.7.29zm-14.28 0a1.024 1.024 0 01-.71-.29 1 1 0 010-1.41l.13-.13a1 1 0 011.41 1.41l-.13.13a1 1 0 01-.7.29zM22 13h-.08a1 1 0 010-2 1.038 1.038 0 011.04 1 .969.969 0 01-.96 1zM2.08 13H2a1 1 0 010-2 1.038 1.038 0 011.04 1 .969.969 0 01-.96 1zm16.93-7.01a1.024 1.024 0 01-.71-.29 1 1 0 010-1.41l.13-.13a1 1 0 011.41 1.41l-.13.13a.984.984 0 01-.7.29zm-14.02 0a1.024 1.024 0 01-.71-.29l-.13-.14a1 1 0 011.41-1.41l.13.13a1 1 0 010 1.41.97.97 0 01-.7.3zM12 3.04a.969.969 0 01-1-.96V2a1 1 0 012 0 1.038 1.038 0 01-1 1.04z" />
    </g>
  </svg>
);

export const SearchIcon = ({
  size = 24,
  width,
  height,
  ...props
}: IconSvgProps) => (
  <svg
    aria-hidden="true"
    focusable="false"
    height={size || height}
    role="presentation"
    viewBox="0 0 24 24"
    width={size || width}
    {...props}
  >
    <path
      d="M11.5 21C16.7467 21 21 16.7467 21 11.5C21 6.25329 16.7467 2 11.5 2C6.25329 2 2 6.25329 2 11.5C2 16.7467 6.25329 21 11.5 21Z"
      fill="none"
      stroke="currentColor"
      strokeLinecap="round"
      strokeLinejoin="round"
      strokeWidth="2"
    />
    <path
      d="M22 22L20.5 20.5"
      stroke="currentColor"
      strokeLinecap="round"
      strokeLinejoin="round"
      strokeWidth="2"
    />
  </svg>
);

export const PlusIcon = ({
  size = 24,
  width,
  height,
  ...props
}: IconSvgProps) => (
  <svg
    aria-hidden="true"
    focusable="false"
    height={size || height}
    role="presentation"
    viewBox="0 0 24 24"
    width={size || width}
    {...props}
  >
    <path
      d="M12 5V19M5 12H19"
      stroke="currentColor"
      strokeLinecap="round"
      strokeLinejoin="round"
      strokeWidth="2"
    />
  </svg>
);

export const EditIcon = ({
  size = 24,
  width,
  height,
  ...props
}: IconSvgProps) => (
  <svg
    aria-hidden="true"
    focusable="false"
    height={size || height}
    role="presentation"
    viewBox="0 0 24 24"
    width={size || width}
    {...props}
  >
    <path
      d="M11 4H4C3.46957 4 2.96086 4.21071 2.58579 4.58579C2.21071 4.96086 2 5.46957 2 6V20C2 20.5304 2.21071 21.0391 2.58579 21.4142C2.96086 21.7893 3.46957 22 4 22H18C18.5304 22 19.0391 21.7893 19.4142 21.4142C19.7893 21.0391 20 20.5304 20 20V13"
      fill="none"
      stroke="currentColor"
      strokeLinecap="round"
      strokeLinejoin="round"
      strokeWidth="2"
    />
    <path
      d="M18.5 2.49998C18.8978 2.10216 19.4374 1.87866 20 1.87866C20.5626 1.87866 21.1022 2.10216 21.5 2.49998C21.8978 2.89781 22.1213 3.43737 22.1213 3.99998C22.1213 4.56259 21.8978 5.10216 21.5 5.49998L12 15L8 16L9 12L18.5 2.49998Z"
      fill="none"
      stroke="currentColor"
      strokeLinecap="round"
      strokeLinejoin="round"
      strokeWidth="2"
    />
  </svg>
);

export const DeleteIcon = ({
  size = 24,
  width,
  height,
  ...props
}: IconSvgProps) => (
  <svg
    aria-hidden="true"
    focusable="false"
    height={size || height}
    role="presentation"
    viewBox="0 0 24 24"
    width={size || width}
    {...props}
  >
    <path
      d="M3 6H5H21"
      stroke="currentColor"
      strokeLinecap="round"
      strokeLinejoin="round"
      strokeWidth="2"
    />
    <path
      d="M8 6V4C8 3.46957 8.21071 2.96086 8.58579 2.58579C8.96086 2.21071 9.46957 2 10 2H14C14.5304 2 15.0391 2.21071 15.4142 2.58579C15.7893 2.96086 16 3.46957 16 4V6M19 6V20C19 20.5304 18.7893 21.0391 18.4142 21.4142C18.0391 21.7893 17.5304 22 17 22H7C6.46957 22 5.96086 21.7893 5.58579 21.4142C5.21071 21.0391 5 20.5304 5 20V6H19Z"
      fill="none"
      stroke="currentColor"
      strokeLinecap="round"
      strokeLinejoin="round"
      strokeWidth="2"
    />
  </svg>
);

export const UserIcon = ({
  size = 24,
  width,
  height,
  ...props
}: IconSvgProps) => (
  <svg
    aria-hidden="true"
    focusable="false"
    height={size || height}
    role="presentation"
    viewBox="0 0 24 24"
    width={size || width}
    {...props}
  >
    <path
      d="M20 21V19C20 17.9391 19.5786 16.9217 18.8284 16.1716C18.0783 15.4214 17.0609 15 16 15H8C6.93913 15 5.92172 15.4214 5.17157 16.1716C4.42143 16.9217 4 17.9391 4 19V21"
      fill="none"
      stroke="currentColor"
      strokeLinecap="round"
      strokeLinejoin="round"
      strokeWidth="2"
    />
    <path
      d="M12 11C14.2091 11 16 9.20914 16 7C16 4.79086 14.2091 3 12 3C9.79086 3 8 4.79086 8 7C8 9.20914 9.79086 11 12 11Z"
      fill="none"
      stroke="currentColor"
      strokeLinecap="round"
      strokeLinejoin="round"
      strokeWidth="2"
    />
  </svg>
);

export const SettingsIcon = ({
  size = 24,
  width,
  height,
  ...props
}: IconSvgProps) => (
  <svg
    aria-hidden="true"
    focusable="false"
    height={size || height}
    role="presentation"
    viewBox="0 0 24 24"
    width={size || width}
    {...props}
  >
    <path
      d="M12.22 2H11.78C11.2496 2 10.7409 2.21071 10.3658 2.58579C9.99072 2.96086 9.78 3.46957 9.78 4C9.78 4.53043 9.99072 5.03914 10.3658 5.41421C10.7409 5.78929 11.2496 6 11.78 6H12.22C12.7504 6 13.2591 5.78929 13.6342 5.41421C14.0093 5.03914 14.22 4.53043 14.22 4C14.22 3.46957 14.0093 2.96086 13.6342 2.58579C13.2591 2.21071 12.7504 2 12.22 2V2Z"
      fill="none"
      stroke="currentColor"
      strokeLinecap="round"
      strokeLinejoin="round"
      strokeWidth="2"
    />
    <path
      d="M19.5 8.5C19.8978 8.10218 20.2374 7.63877 20.5 7.12132C20.7626 6.60387 20.8478 6.03677 20.8478 5.464C20.8478 4.89123 20.7626 4.32413 20.5 3.80668C20.2374 3.28923 19.8978 2.82582 19.5 2.428C19.1022 2.03018 18.6388 1.69063 18.1213 1.42801C17.6039 1.16539 17.0368 1.08023 16.464 1.08023C15.8912 1.08023 15.3241 1.16539 14.8067 1.42801C14.2892 1.69063 13.8258 2.03018 13.428 2.428L12 3.856L10.572 2.428C9.78889 1.64486 8.73968 1.21408 7.64800 1.21408C6.55632 1.21408 5.50711 1.64486 4.72400 2.428C3.94086 3.21111 3.51008 4.26032 3.51008 5.352C3.51008 6.44368 3.94086 7.49289 4.72400 8.276L6.15200 9.704L12 15.552L17.848 9.704L19.276 8.276C19.5 8.052 19.5 8.276 19.5 8.5Z"
      fill="none"
      stroke="currentColor"
      strokeLinecap="round"
      strokeLinejoin="round"
      strokeWidth="2"
    />
  </svg>
);

export const BackIcon = ({
  size = 24,
  width,
  height,
  ...props
}: IconSvgProps) => (
  <svg
    aria-hidden="true"
    focusable="false"
    height={size || height}
    role="presentation"
    viewBox="0 0 24 24"
    width={size || width}
    {...props}
  >
    <path
      d="M15 19l-7-7 7-7"
      fill="none"
      stroke="currentColor"
      strokeLinecap="round"
      strokeLinejoin="round"
      strokeWidth="2"
    />
  </svg>
);
