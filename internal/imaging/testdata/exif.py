"""Makes the pictures for the EXIF tests. The pictures are drawn, not taken.

Run: python3 exif.py   (needs Pillow)
"""
from PIL import Image

def picture():
    img = Image.new("RGB", (48, 32))
    img.putdata([(x * 5, y * 7, 120) for y in range(32) for x in range(48)])
    return img

def save(name, fill):
    img = picture()
    exif = Image.Exif()
    fill(exif)
    img.save(name, "JPEG", quality=70, exif=exif)

def camera(exif):
    exif[0x010F] = "Example"
    exif[0x0110] = "Example Cam 3"
    exif[0x0112] = 6
    exif[0x0132] = "2021:03:04 10:11:12"
    exif[0x8769] = {0x9003: "2019:07:14 15:42:10"}
    # An invented point in the sea: 12 deg 30' 36" south, 45 deg 15' 0" west.
    exif[0x8825] = {1: "S", 2: (12.0, 30.0, 36.0), 3: "W", 4: (45.0, 15.0, 0.0)}

def town(exif):
    exif[0x8769] = {0x9003: "2022:05:01 12:00:00"}
    # A well-known tower in a capital: 48 deg 51' 30" north, 2 deg 17' 40" east.
    exif[0x8825] = {1: "N", 2: (48.0, 51.0, 30.0), 3: "E", 4: (2.0, 17.0, 40.0)}

def scanner(exif):
    exif[0x010F] = "Example"
    exif[0x0110] = "Scan 9"
    exif[0x0132] = "2024:11:05 09:00:00"

def no_signal(exif):
    exif[0x0132] = "0000:00:00 00:00:00"
    exif[0x8825] = {1: "N", 2: (0.0, 0.0, 0.0), 3: "E", 4: (0.0, 0.0, 0.0)}

save("exif-camera.jpg", camera)
save("exif-town.jpg", town)
save("exif-scanner.jpg", scanner)
save("exif-no-signal.jpg", no_signal)
picture().save("exif-none.jpg", "JPEG", quality=70)
