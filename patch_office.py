import re

with open('desktop/src/components/OfficeScene.tsx', 'r') as f:
    content = f.read()

# 1. Fix inner walls
old_walls = """        // Inner Horizontal Row 9
        drawWall(2, 9, 3, 1)
        drawWall(7, 9, 3, 1)
        drawWall(10, 9, 1, 1) // cross
        drawWall(11, 9, 4, 1)
        drawWall(17, 9, 3, 1)
        drawWall(20, 9, 1, 1) // cross
        drawWall(21, 9, 4, 1)
        drawWall(27, 9, 3, 1)

        // Inner Horizontal Row 12
        drawWall(2, 12, 3, 1)
        drawWall(7, 12, 3, 1)
        drawWall(10, 12, 1, 1) // cross
        drawWall(21, 12, 4, 1)
        drawWall(29, 12, 1, 1) // leaves 25~27 as door, 27~29 as elevatorawWall(20, 12, 1, 1) // cross
        drawWall(21, 12, 4, 1)
        drawWall(27, 12, 3, 1)"""
new_walls = """        // Inner Horizontal Row 9
        drawWall(2, 9, 3, 1)
        drawWall(7, 9, 3, 1)
        drawWall(10, 9, 1, 1) // cross
        drawWall(11, 9, 4, 1)
        drawWall(17, 9, 3, 1)
        drawWall(20, 9, 1, 1) // cross
        drawWall(21, 9, 4, 1)

        // Inner Horizontal Row 12
        drawWall(2, 12, 3, 1)
        drawWall(7, 12, 3, 1)
        drawWall(10, 12, 1, 1) // cross
        drawWall(11, 12, 4, 1)
        drawWall(17, 12, 3, 1)
        drawWall(20, 12, 1, 1) // cross
        drawWall(21, 12, 4, 1)

        // Elevator Shaft (Thick block)
        drawWall(27, 9, 3, 4)"""
content = content.replace(old_walls, new_walls)

# 2. Fix Entrance
old_entrance = """          entrance.x = 28 * GRID_SIZE
          entrance.y = 13.0 * GRID_SIZE + 16 // Built into the top wall of lobby (y=13.5)"""
new_entrance = """          entrance.x = 28.5 * GRID_SIZE
          entrance.y = 13.0 * GRID_SIZE - 48.5 // Flush with lobby floor (gy=13)"""
content = content.replace(old_entrance, new_entrance)

# 3. Fix Desk Mask
old_mask = """          // Mask foreground desk to hide the chair backrest (top part of image)
          const deskMask = new Graphics()
          deskMask.rect(-55, -15, 110, deskHeight) // -15 starts below the chair backrest
          deskMask.fill(0xffffff)
          deskFg.mask = deskMask
          deskFg.addChild(deskMask)"""
new_mask = """          // Mask foreground desk to hide the chair backrest (top part of image)
          const deskMask = new Graphics()
          // Left side
          deskMask.rect(-55, -35, 30, deskHeight + 35)
          // Right side
          deskMask.rect(25, -35, 30, deskHeight + 35)
          // Front side
          deskMask.rect(-25, -2.7, 50, deskHeight + 35)
          deskMask.fill(0xffffff)
          deskFg.mask = deskMask
          deskFg.addChild(deskMask)"""
content = content.replace(old_mask, new_mask)

# 4. Fix Agent Mask
old_agent = """      // Apply mask and offset for L1 agent to sit in the executive chair
      if (agent.workstationId === 'desk-L1' && agent.status === 'working') {
        data.sprite.x = -14
        data.sprite.y = -10 // Just sit slightly up in the chair
        if (data.mask) {
          data.sprite.mask = null
          data.container.removeChild(data.mask)
          data.mask.destroy()
          data.mask = undefined
        }
      }"""
new_agent = """      // Apply mask and offset for L1 agent to sit in the executive chair
      if (agent.workstationId === 'desk-L1' && agent.status === 'working') {
        data.sprite.x = 0
        data.sprite.y = -6 // Move agent up to sit properly in the chair
        if (!data.mask) {
          const m = new Graphics()
          m.rect(-40, -60, 80, 49.3) // Cut off at y = -10.7 relative to container (matches desk inner edge)
          m.fill(0xffffff)
          data.container.addChild(m)
          data.sprite.mask = m
          data.mask = m
        }
      }"""
content = content.replace(old_agent, new_agent)

# 5. Fix Plants
content = re.sub(r'        drawPlant\(2\.5, 3\.0\).*?drawPlant\(28\.5, 11\.2\) // Corridor right\n',
"""        drawPlant(2.5, 3.0)
        drawPlant(9.5, 3.0)
        drawPlant(11.5, 3.0)
        drawPlant(18.5, 3.0)
        drawPlant(21.5, 3.0)
        drawPlant(28.5, 3.0)
        drawPlant(2.5, 14.5)
        drawPlant(2.5, 18.5)
        drawPlant(19.5, 14.5)
        drawPlant(22.5, 18.5)
        drawPlant(28.5, 18.5)
""", content, flags=re.DOTALL)

with open('desktop/src/components/OfficeScene.tsx', 'w') as f:
    f.write(content)
print("OfficeScene.tsx patched successfully.")
