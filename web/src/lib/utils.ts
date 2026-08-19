import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

export function shortGitSHA(value: string): string {
  return value ? value.slice(0, 8) : '—'
}
