# Image decoder regression fixtures

These two images contain generated pixels and no user content. They were created
with Pillow 11.3.0 for the x/image dependency regression:

```python
from PIL import Image
import random
r = random.Random(924819)
Image.frombytes('RGB', (128, 128), bytes(r.randrange(256) for _ in range(128*128*3))).save('avatar-lossless.webp', format='WEBP', lossless=True)
Image.new('RGB', (1320, 500), (27, 108, 158)).save('skin-lossless.webp', format='WEBP', lossless=True)
```

The avatar exceeds the 20 KiB compression threshold and stays below the 100 KiB
upload limit. The skin uses the existing 1320 x 500 dimensions. The avatar test
adds a conflicting VP8X canvas to exercise invalid-image rejection. This is a
decoder compatibility regression, not a claim that every historical denial of
service exploit was reproduced.
