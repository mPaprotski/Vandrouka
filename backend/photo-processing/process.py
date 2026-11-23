"""Apply Deface's CenterFace detector to a validated visit photo."""
import math
import sys

import cv2
from deface.centerface import CenterFace


def process(source, output):
    cv2.setNumThreads(1)
    frame = cv2.imread(source, cv2.IMREAD_UNCHANGED)
    if frame is None:
        raise ValueError("invalid image")

    height, width = frame.shape[:2]
    scale = min(1, 640 / max(width, height))
    shape = (max(32, round(width * scale)), max(32, round(height * scale)))
    small = cv2.resize(frame, shape, interpolation=cv2.INTER_AREA)
    if small.ndim == 2:
        rgb = cv2.cvtColor(small, cv2.COLOR_GRAY2RGB)
    else:
        rgb = cv2.cvtColor(small, cv2.COLOR_BGR2RGB)

    detector = CenterFace(backend="opencv")
    faces, _ = detector(rgb, threshold=0.2)
    scale_x, scale_y = width / shape[0], height / shape[1]
    for x1, y1, x2, y2, _ in faces:
        pad_x, pad_y = (x2 - x1) * 0.3, (y2 - y1) * 0.3
        left = max(0, math.floor((x1 - pad_x) * scale_x))
        top = max(0, math.floor((y1 - pad_y) * scale_y))
        right = min(width, math.ceil((x2 + pad_x) * scale_x))
        bottom = min(height, math.ceil((y2 + pad_y) * scale_y))
        if right <= left or bottom <= top:
            continue
        region = frame[top:bottom, left:right]
        frame[top:bottom, left:right] = cv2.blur(
            region, (max(1, (right - left) // 2), max(1, (bottom - top) // 2)),
            borderType=cv2.BORDER_REPLICATE | cv2.BORDER_ISOLATED,
        )

    params = [cv2.IMWRITE_PNG_COMPRESSION, 1] if output.endswith(".png") else []
    if not cv2.imwrite(output, frame, params):
        raise OSError("could not encode image")


if __name__ == "__main__":
    process(*sys.argv[1:])
