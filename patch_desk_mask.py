import re

with open('desktop/src/components/OfficeScene.tsx', 'r') as f:
    content = f.read()

# Fix Desk Mask
old_mask = """          // Mask foreground desk to hide the chair backrest (top part of image)
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

new_mask = """          // Mask foreground desk to hide the chair backrest (top part of image)
          const deskMask = new Graphics()
          // Left side
          deskMask.rect(-55, -35, 33, deskHeight + 35)
          // Right side
          deskMask.rect(22, -35, 33, deskHeight + 35)
          // Front side
          deskMask.rect(-22, -13.5, 44, deskHeight + 35)
          deskMask.fill(0xffffff)
          deskFg.mask = deskMask
          deskFg.addChild(deskMask)"""
content = content.replace(old_mask, new_mask)

# Fix Agent Mask
old_agent = """      // Apply mask and offset for L1 agent to sit in the executive chair
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

new_agent = """      // Apply mask and offset for L1 agent to sit in the executive chair
      if (agent.workstationId === 'desk-L1' && agent.status === 'working') {
        data.sprite.x = 0
        data.sprite.y = -6 // Move agent up to sit properly in the chair
        if (data.mask) {
          data.sprite.mask = null
          data.container.removeChild(data.mask)
          data.mask.destroy()
          data.mask = undefined
        }
      }"""
content = content.replace(old_agent, new_agent)

with open('desktop/src/components/OfficeScene.tsx', 'w') as f:
    f.write(content)
print("Desk mask patched successfully.")
