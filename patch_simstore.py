import re

with open('desktop/src/stores/simStore.ts', 'r') as f:
    content = f.read()

old_elevator = """    // Elevator area (visually at 27~29, 13~15)
    if (gx >= 27 && gx <= 29 && gy >= 13 && gy <= 15) return false;"""
new_elevator = """    // Elevator shaft
    if (gx >= 27 && gx <= 29 && gy >= 9 && gy <= 12) return false;"""
content = content.replace(old_elevator, new_elevator)

with open('desktop/src/stores/simStore.ts', 'w') as f:
    f.write(content)
print("simStore.ts patched successfully.")
