declare module 'tracking-number-validation' {
  export function getCourier(trackingNumber: string): string[];
  export function getCourierOne(trackingNumber: string): string | null;
  export function isCourier(trackingNumber: string, courier: string): boolean;
  export function getTrackingUrl(trackingNumber: string): string | null;
  export function isValid(trackingNumber: string): boolean;
}
