import React from 'react';

interface PivotHeaderProps {
  title: string;
  subtitle?: string;
}

const PivotHeader: React.FC<PivotHeaderProps> = ({ title, subtitle }) => {
  return (
    <div className="mb-6 ml-1 animate-[pageIn_0.6s_ease-out_forwards]">
      {subtitle && (
        <h2 className="text-sm font-semibold tracking-widest uppercase text-gray-400 mb-0">
          {subtitle}
        </h2>
      )}
      <h1 className="text-6xl font-thin tracking-tight text-white -ml-1">
        {title}
      </h1>
    </div>
  );
};

export default PivotHeader;