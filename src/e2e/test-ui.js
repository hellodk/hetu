const puppeteer = require('puppeteer');
const fs = require('fs');
const path = require('path');

const delay = ms => new Promise(resolve => setTimeout(resolve, ms));

async function testApplication() {
  console.log('Starting browser...');
  const browser = await puppeteer.launch({
    headless: true,
    args: ['--no-sandbox', '--disable-setuid-sandbox']
  });
  
  const page = await browser.newPage();
  await page.setViewport({ width: 1920, height: 1080 });
  
  // Listen to console messages
  page.on('console', msg => console.log('PAGE LOG:', msg.text()));
  
  // Listen to network requests
  page.on('requestfailed', request => {
    console.log('REQUEST FAILED:', request.url(), request.failure().errorText);
  });
  
  const screenshotDir = path.join(__dirname, 'test-screenshots');
  if (!fs.existsSync(screenshotDir)) {
    fs.mkdirSync(screenshotDir);
  }
  
  try {
    console.log('\n=== Test 1: Overview Page ===');
    await page.goto('http://localhost:8080', { waitUntil: 'networkidle2', timeout: 15000 });
    
    // Wait a bit for API calls
    console.log('Waiting for API calls to complete...');
    await delay(5000);
    
    await page.screenshot({ path: path.join(screenshotDir, '1-overview.png'), fullPage: true });
    console.log('✓ Screenshot saved: 1-overview.png');
    
    // Get cluster health score and summary info
    const pageInfo = await page.evaluate(() => {
      const results = {
        healthScore: 'Not found',
        title: document.title,
        headings: [],
        tabs: [],
        bodyText: document.body.textContent.substring(0, 500)
      };
      
      // Get title/heading
      const h1 = document.querySelector('h1, h2, [class*="text-3xl"]');
      if (h1) results.headings.push(h1.textContent.trim());
      
      // Try to find health score - look for large numbers
      const allElements = document.querySelectorAll('*');
      for (const el of allElements) {
        const text = el.textContent.trim();
        // Look for standalone numbers that might be scores
        if (el.children.length === 0 && text.match(/^\d{1,3}$/)) {
          const fontSize = window.getComputedStyle(el).fontSize;
          if (parseInt(fontSize) > 30) {
            results.healthScore = text;
            break;
          }
        }
      }
      
      // Get all button texts to find tabs
      const buttons = document.querySelectorAll('button');
      buttons.forEach(btn => {
        const text = btn.textContent.trim();
        if (text && text.length > 0 && text.length < 50) {
          results.tabs.push(text);
        }
      });
      
      return results;
    });
    
    console.log(`Page Title: ${pageInfo.title}`);
    console.log(`Cluster Health Score: ${pageInfo.healthScore}`);
    console.log(`Headings: ${pageInfo.headings.join(', ')}`);
    console.log(`Available tabs/buttons: ${pageInfo.tabs.slice(0, 15).join(', ')}`);
    console.log(`Body text preview: ${pageInfo.bodyText.substring(0, 200)}`);
    
    console.log('\n=== Test 2: Pod Health Tab ===');
    // Find and click Pod Health tab
    const podHealthClicked = await page.evaluate(() => {
      const buttons = document.querySelectorAll('button');
      for (const btn of buttons) {
        const text = btn.textContent.trim();
        if (text.includes('Pod') && (text.includes('Health') || text.includes('Status'))) {
          btn.click();
          return text;
        }
      }
      // Try just "Pods" or similar
      for (const btn of buttons) {
        const text = btn.textContent.trim();
        if (text === 'Pods' || text === 'Pod Health') {
          btn.click();
          return text;
        }
      }
      return null;
    });
    
    if (podHealthClicked) {
      console.log(`✓ Clicked tab: ${podHealthClicked}`);
      await delay(2000);
      await page.screenshot({ path: path.join(screenshotDir, '2-pod-health.png'), fullPage: true });
      console.log('✓ Screenshot saved: 2-pod-health.png');
      
      // Get info about what's on this tab
      const podInfo = await page.evaluate(() => {
        const buttons = document.querySelectorAll('button');
        const buttonTexts = [];
        buttons.forEach(btn => {
          const text = btn.textContent.trim();
          if (text && text.length > 0 && text.length < 30) {
            buttonTexts.push(text);
          }
        });
        return { buttons: buttonTexts };
      });
      console.log(`Buttons on Pod Health tab: ${podInfo.buttons.slice(0, 15).join(', ')}`);
      
      // Try to find and click a Diagnose button
      const diagnoseClicked = await page.evaluate(() => {
        const buttons = document.querySelectorAll('button');
        for (const btn of buttons) {
          if (btn.textContent.includes('Diagnose')) {
            btn.click();
            return true;
          }
        }
        return false;
      });
      
      if (diagnoseClicked) {
        console.log('✓ Clicked Diagnose button');
        await delay(2000);
        await page.screenshot({ path: path.join(screenshotDir, '3-diagnosis-modal.png'), fullPage: true });
        console.log('✓ Screenshot saved: 3-diagnosis-modal.png');
        
        // Get modal content info
        const modalInfo = await page.evaluate(() => {
          const headings = [];
          document.querySelectorAll('h1, h2, h3, h4').forEach(h => {
            headings.push(h.textContent.trim());
          });
          return { headings };
        });
        console.log(`Modal headings: ${modalInfo.headings.join(', ')}`);
        
        // Try to close modal
        await page.evaluate(() => {
          const buttons = document.querySelectorAll('button');
          for (const btn of buttons) {
            const text = btn.textContent.trim();
            if (text === 'Close' || text === '×' || text === 'Cancel') {
              btn.click();
              break;
            }
          }
        });
        await delay(500);
      } else {
        console.log('⚠ No Diagnose button found');
      }
    } else {
      console.log('⚠ Pod Health tab not found');
      console.log('Available buttons:', pageInfo.tabs.join(', '));
    }
    
    console.log('\n=== Test 3: Resources Tab ===');
    // Find and click Resources tab
    const resourcesClicked = await page.evaluate(() => {
      const buttons = document.querySelectorAll('button');
      for (const btn of buttons) {
        const text = btn.textContent.trim();
        if (text.includes('Resource')) {
          btn.click();
          return text;
        }
      }
      return null;
    });
    
    if (resourcesClicked) {
      console.log(`✓ Clicked tab: ${resourcesClicked}`);
      await delay(2000);
      await page.screenshot({ path: path.join(screenshotDir, '4-resources.png'), fullPage: true });
      console.log('✓ Screenshot saved: 4-resources.png');
      
      // Get info about what's on this tab
      const resourceInfo = await page.evaluate(() => {
        const buttons = document.querySelectorAll('button');
        const buttonTexts = [];
        buttons.forEach(btn => {
          const text = btn.textContent.trim();
          if (text && text.length > 0 && text.length < 30) {
            buttonTexts.push(text);
          }
        });
        return { buttons: buttonTexts };
      });
      console.log(`Buttons on Resources tab: ${resourceInfo.buttons.slice(0, 15).join(', ')}`);
      
      // Look for Details or Apply buttons
      const resourceButtonClicked = await page.evaluate(() => {
        const buttons = document.querySelectorAll('button');
        for (const btn of buttons) {
          const text = btn.textContent.trim();
          if (text.includes('Details') || text.includes('Apply')) {
            btn.click();
            return text;
          }
        }
        return null;
      });
      
      if (resourceButtonClicked) {
        console.log(`✓ Clicked ${resourceButtonClicked} button`);
        await delay(2000);
        await page.screenshot({ path: path.join(screenshotDir, '5-resource-detail-modal.png'), fullPage: true });
        console.log('✓ Screenshot saved: 5-resource-detail-modal.png');
        
        // Get modal content info
        const modalInfo = await page.evaluate(() => {
          const headings = [];
          document.querySelectorAll('h1, h2, h3, h4').forEach(h => {
            headings.push(h.textContent.trim());
          });
          return { headings };
        });
        console.log(`Modal headings: ${modalInfo.headings.join(', ')}`);
      } else {
        console.log('⚠ No Details/Apply button found');
      }
    } else {
      console.log('⚠ Resources tab not found');
      console.log('Available buttons:', pageInfo.tabs.join(', '));
    }
    
    console.log('\n=== Test Complete ===');
    console.log(`Screenshots saved to: ${screenshotDir}`);
    
  } catch (error) {
    console.error('Error during testing:', error);
    await page.screenshot({ path: path.join(screenshotDir, 'error.png'), fullPage: true });
  } finally {
    await browser.close();
  }
}

testApplication().catch(console.error);
