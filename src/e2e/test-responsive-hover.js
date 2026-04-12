const puppeteer = require('puppeteer');

(async () => {
  console.log('🚀 Testing responsive layout and hover effects...\n');
  
  const browser = await puppeteer.launch({
    headless: true,
    args: ['--no-sandbox', '--disable-setuid-sandbox']
  });
  
  const page = await browser.newPage();
  
  try {
    // Test at desktop resolution
    console.log('📱 Testing at desktop resolution (1920x1080)...');
    await page.setViewport({ width: 1920, height: 1080 });
    await page.goto('http://localhost:3000', { waitUntil: 'networkidle0', timeout: 10000 });
    
    // Wait for error state to appear
    await new Promise(resolve => setTimeout(resolve, 2000));
    
    // Check header layout
    const headerInfo = await page.evaluate(() => {
      const header = document.querySelector('header');
      if (!header) return { found: false };
      
      const title = header.querySelector('h1, .text-xl, [class*="text-"]');
      const buttons = header.querySelectorAll('button');
      const buttonContainer = Array.from(header.querySelectorAll('div')).find(div => {
        const btns = div.querySelectorAll('button');
        return btns.length >= 2;
      });
      
      return {
        found: true,
        hasTitle: !!title,
        titleText: title?.textContent || '',
        buttonCount: buttons.length,
        buttonsGrouped: !!buttonContainer,
        headerClasses: header.className
      };
    });
    
    console.log('   Header found:', headerInfo.found);
    console.log('   Title:', headerInfo.titleText);
    console.log('   Buttons:', headerInfo.buttonCount);
    console.log('   Buttons grouped:', headerInfo.buttonsGrouped);
    
    await page.screenshot({ path: '/tmp/dashboard-desktop.png', fullPage: true });
    console.log('   📸 Screenshot: /tmp/dashboard-desktop.png\n');
    
    // Test tablet resolution
    console.log('📱 Testing at tablet resolution (768x1024)...');
    await page.setViewport({ width: 768, height: 1024 });
    await new Promise(resolve => setTimeout(resolve, 500));
    await page.screenshot({ path: '/tmp/dashboard-tablet.png', fullPage: true });
    console.log('   📸 Screenshot: /tmp/dashboard-tablet.png\n');
    
    // Test mobile resolution
    console.log('📱 Testing at mobile resolution (375x667)...');
    await page.setViewport({ width: 375, height: 667 });
    await new Promise(resolve => setTimeout(resolve, 500));
    await page.screenshot({ path: '/tmp/dashboard-mobile.png', fullPage: true });
    console.log('   📸 Screenshot: /tmp/dashboard-mobile.png\n');
    
    // Back to desktop for hover tests
    await page.setViewport({ width: 1920, height: 1080 });
    await new Promise(resolve => setTimeout(resolve, 500));
    
    // Test hover effects on retry button
    console.log('🎨 Testing hover effects...');
    
    const retryButton = await page.$('button');
    if (retryButton) {
      // Get initial state
      const initialState = await retryButton.evaluate(el => {
        const styles = window.getComputedStyle(el);
        return {
          transform: styles.transform,
          boxShadow: styles.boxShadow,
          backgroundColor: styles.backgroundColor,
          scale: styles.scale
        };
      });
      
      console.log('   Initial button state:', initialState);
      
      // Hover over button
      await retryButton.hover();
      await new Promise(resolve => setTimeout(resolve, 300));
      
      const hoverState = await retryButton.evaluate(el => {
        const styles = window.getComputedStyle(el);
        return {
          transform: styles.transform,
          boxShadow: styles.boxShadow,
          backgroundColor: styles.backgroundColor,
          scale: styles.scale
        };
      });
      
      console.log('   Hover button state:', hoverState);
      
      await page.screenshot({ path: '/tmp/dashboard-button-hover.png', fullPage: true });
      console.log('   📸 Screenshot: /tmp/dashboard-button-hover.png');
      
      // Check if hover effect is present
      if (initialState.transform !== hoverState.transform ||
          initialState.backgroundColor !== hoverState.backgroundColor ||
          initialState.boxShadow !== hoverState.boxShadow) {
        console.log('   ✅ Hover effects detected on button!\n');
      } else {
        console.log('   ⚠️  No visible hover effects detected\n');
      }
    }
    
    // Test card hover effects (if any cards are visible)
    const cards = await page.$$('.bg-cluster-card, [class*="card"]');
    console.log(`   Found ${cards.length} card elements`);
    
    if (cards.length > 0) {
      const card = cards[0];
      
      const initialCardState = await card.evaluate(el => {
        const styles = window.getComputedStyle(el);
        return {
          transform: styles.transform,
          boxShadow: styles.boxShadow
        };
      });
      
      await card.hover();
      await new Promise(resolve => setTimeout(resolve, 300));
      
      const hoverCardState = await card.evaluate(el => {
        const styles = window.getComputedStyle(el);
        return {
          transform: styles.transform,
          boxShadow: styles.boxShadow
        };
      });
      
      await page.screenshot({ path: '/tmp/dashboard-card-hover.png', fullPage: true });
      console.log('   📸 Screenshot: /tmp/dashboard-card-hover.png');
      
      if (initialCardState.transform !== hoverCardState.transform ||
          initialCardState.boxShadow !== hoverCardState.boxShadow) {
        console.log('   ✅ Hover effects detected on cards!\n');
      } else {
        console.log('   ℹ️  No transform/shadow changes on cards\n');
      }
    }
    
    console.log('✅ Responsive and hover tests complete!');
    
  } catch (error) {
    console.error('\n❌ Error during testing:', error.message);
  } finally {
    await browser.close();
  }
})();
