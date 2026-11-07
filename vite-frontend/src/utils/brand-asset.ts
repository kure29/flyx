export type BrandAssetKind = "logo" | "favicon";

const MAX_BRAND_UPLOAD_BYTES = 2 * 1024 * 1024;
const PNG_DATA_URL_PREFIX = "data:image/png;base64,";

const OUTPUT_SIZE: Record<BrandAssetKind, number> = {
  logo: 96,
  favicon: 64,
};

const readFileAsDataURL = (file: File): Promise<string> => {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();

    reader.onload = () => {
      if (typeof reader.result !== "string") {
        reject(new Error("读取图片失败"));

        return;
      }

      resolve(reader.result);
    };

    reader.onerror = () => reject(new Error("读取图片失败"));
    reader.readAsDataURL(file);
  });
};

const loadImage = (src: string): Promise<HTMLImageElement> => {
  return new Promise((resolve, reject) => {
    const image = new Image();

    image.onload = () => resolve(image);
    image.onerror = () => reject(new Error("图片解析失败"));
    image.src = src;
  });
};

const drawContainedPNG = (
  image: HTMLImageElement,
  size: number,
): string | null => {
  const canvas = document.createElement("canvas");

  canvas.width = size;
  canvas.height = size;

  const ctx = canvas.getContext("2d");

  if (!ctx) {
    return null;
  }

  ctx.clearRect(0, 0, size, size);

  const sourceWidth = image.naturalWidth || image.width;
  const sourceHeight = image.naturalHeight || image.height;

  if (sourceWidth <= 0 || sourceHeight <= 0) {
    return null;
  }

  const scale = Math.min(size / sourceWidth, size / sourceHeight);
  const drawWidth = sourceWidth * scale;
  const drawHeight = sourceHeight * scale;
  const drawX = (size - drawWidth) / 2;
  const drawY = (size - drawHeight) / 2;

  ctx.imageSmoothingEnabled = true;
  ctx.imageSmoothingQuality = "high";
  ctx.drawImage(image, drawX, drawY, drawWidth, drawHeight);

  return canvas.toDataURL("image/png");
};

export const isPngDataURL = (value: string): boolean => {
  return value.startsWith(PNG_DATA_URL_PREFIX);
};

export const convertBrandAssetToPngDataURL = async (
  file: File,
  kind: BrandAssetKind,
): Promise<string> => {
  if (!file.type.startsWith("image/")) {
    throw new Error("仅支持上传图片文件");
  }

  if (file.size > MAX_BRAND_UPLOAD_BYTES) {
    throw new Error("图片过大，请上传 2MB 以内的文件");
  }

  const sourceDataURL = await readFileAsDataURL(file);
  const image = await loadImage(sourceDataURL);
  const output = drawContainedPNG(image, OUTPUT_SIZE[kind]);

  if (!output) {
    throw new Error("图片处理失败，请重试");
  }

  return output;
};
